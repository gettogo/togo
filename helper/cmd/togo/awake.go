package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/awake"
	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/doctor"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/state"
)

func init() {
	register("awake", func(ctx context.Context, args []string, _ map[string]string, stdin io.Reader, stdout, stderr io.Writer) int {
		return runAwake(ctx, args, stdin, stdout, stderr)
	})
	addUsage("  awake [off|plugged|battery] [--battery-floor N]\n" +
		"                                                keep this machine awake while sessions work")
}

// Seams for tests, so no test runs sudo or touches the real power state.
var (
	awakeGOOS = runtime.GOOS
	// awakeAdmin builds the battery mode setup, whose sudo prompts use
	// the terminal.
	awakeAdmin = func(home string, stderr io.Writer) awake.Admin {
		name := ""
		if u, err := user.Current(); err == nil {
			name = u.Username
		}
		return awake.Admin{Home: home, User: name, SudoersPath: awake.DefaultSudoersPath,
			Run: terminalRunner(stderr), Feed: terminalFeeder(stderr)}
	}
	// awakeOverride clears disablesleep without a prompt when no daemon
	// is there to do it.
	awakeOverride = func() awake.SleepOverride { return awake.SudoPmset{} }
)

const awakeCallTimeout = 10 * time.Second

func terminalRunner(stderr io.Writer) awake.Runner {
	return func(ctx context.Context, argv ...string) error {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stderr, stderr
		return cmd.Run()
	}
}

// terminalFeeder runs argv with input on its standard input and its
// output discarded, so tee does not echo the rule it writes.
func terminalFeeder(stderr io.Writer) func(ctx context.Context, input string, argv ...string) error {
	return func(ctx context.Context, input string, argv ...string) error {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(input), io.Discard, stderr
		return cmd.Run()
	}
}

// runAwake shows or changes how this machine is kept awake.
func runAwake(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := newFlagSet("awake")
	floor := fs.Int("battery-floor", 0, "battery percent at or below which battery mode pauses, 10-50")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	floorSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "battery-floor" {
			floorSet = true
		}
	})
	if len(pos) > 1 {
		return usageError(stderr, errors.New("awake takes one mode: off, plugged or battery"))
	}
	var change awake.Change
	if len(pos) == 1 {
		mode, ok := awake.ParseMode(pos[0])
		if !ok {
			return usageError(stderr, fmt.Errorf("unknown mode %q: use off, plugged or battery", pos[0]))
		}
		change.Mode = &mode
	}
	if floorSet {
		if err := awake.ValidateFloor(*floor); err != nil {
			return usageError(stderr, err)
		}
		change.BatteryFloor = floor
	}
	home, err := state.Home()
	if err != nil {
		return fail(stderr, err)
	}
	if change.Mode == nil && change.BatteryFloor == nil {
		return showAwake(ctx, home, stdout, stderr)
	}

	admin := awakeAdmin(home, stderr)
	if change.Mode != nil && *change.Mode == awake.ModeBattery {
		if awakeGOOS != "darwin" && awakeGOOS != "linux" {
			return fail(stderr, fmt.Errorf("keeping awake is not supported on %s", awakeGOOS))
		}
		if awakeGOOS == "darwin" && !admin.Installed() {
			if code := setupBattery(ctx, admin, stdin, stdout, stderr); code != exitOK {
				return code
			}
		}
	}
	st, live, err := setAwake(ctx, home, change)
	if err != nil {
		// The setup may have left disablesleep set for the daemon to
		// clear; with the change failed, nothing will.
		if awakeGOOS == "darwin" {
			if rerr := admin.Restore(ctx); rerr != nil {
				fmt.Fprintf(stderr, "togo: %v\n", rerr)
			}
		}
		return fail(stderr, err)
	}
	if awakeGOOS == "darwin" && change.Mode != nil && *change.Mode != awake.ModeBattery && (admin.Installed() || admin.Marked()) {
		installed := admin.Installed()
		if installed {
			fmt.Fprintf(stdout, "Removing %s; sudo may ask for your password.\n", admin.SudoersPath)
		} else {
			fmt.Fprintln(stdout, "Setting disablesleep back to 0; sudo may ask for your password.")
		}
		if _, err := admin.Remove(ctx); err != nil {
			return fail(stderr, err)
		}
		if installed {
			fmt.Fprintf(stdout, "Removed %s.\n", admin.SudoersPath)
		}
		if live {
			st, _ = refreshAwake(ctx, home, st)
		}
	}
	printAwake(stdout, st, live)
	return exitOK
}

// setupBattery shows the sudoers rule, asks before installing it, checks
// sudo works and installs the rule.
func setupBattery(ctx context.Context, admin awake.Admin, stdin io.Reader, stdout, stderr io.Writer) int {
	rule, err := awake.SudoersRule(admin.User)
	if err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Battery mode keeps this Mac awake with the lid closed, on battery too, by\n"+
		"running pmset -a disablesleep 1 while a session is working or waiting on you,\n"+
		"and disablesleep 0 afterwards. So the helper can do that without a password,\n"+
		"togo installs this rule at %s:\n\n%s\n"+
		"sudo asks for your password once now. Continue? [y/N] ",
		admin.SudoersPath, indent(rule))
	answer, _ := bufio.NewReader(stdin).ReadString('\n')
	fmt.Fprintln(stdout)
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		fmt.Fprintln(stderr, "togo: battery mode not set up; nothing changed")
		return exitError
	}
	if err := admin.Enable(ctx); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Installed %s.\n", admin.SudoersPath)
	return exitOK
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return "  " + strings.Join(lines, "\n  ") + "\n"
}

// setAwake applies change through the daemon. Without one it saves the
// settings for the next start and, since nothing is active, clears a
// disablesleep that Togo left set. live reports whether a daemon answered.
func setAwake(ctx context.Context, home string, change awake.Change) (st awake.Status, live bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, awakeCallTimeout)
	defer cancel()
	err = localCall(ctx, daemon.SocketPath(home), awake.MethodSet, change, &st)
	var derr *daemonError
	switch {
	case err == nil:
		return st, true, nil
	case errors.As(err, &derr) && derr.Code == rpc.CodeRefused:
		return st, true, errors.New("battery mode is not set up on this machine: run togo awake battery here")
	case errors.As(err, &derr) && derr.Code == rpc.CodeMethodNotFound:
		// A daemon from before keeping awake; the saved settings apply
		// once it restarts.
	case derr != nil:
		return st, true, err
	case !errors.Is(err, errUnreachable):
		return st, false, err
	}
	s, _ := awake.LoadSettings(home)
	if change.Mode != nil {
		s.Mode = *change.Mode
	}
	if change.BatteryFloor != nil {
		s.BatteryFloor = *change.BatteryFloor
	}
	if err := awake.SaveSettings(home, s); err != nil {
		return st, false, err
	}
	if awakeGOOS == "darwin" {
		// Turning battery mode off clears through a plain sudo next if
		// this unattended clear cannot.
		leaving := change.Mode != nil && *change.Mode != awake.ModeBattery
		if _, err := awake.ClearOverride(ctx, home, awakeOverride()); err != nil && !leaving {
			return st, false, fmt.Errorf("set disablesleep back to 0: %w", err)
		}
	}
	return offlineStatus(s), false, nil
}

// refreshAwake asks the daemon again after the rule or override changed
// under it, keeping st if it does not answer.
func refreshAwake(ctx context.Context, home string, st awake.Status) (awake.Status, bool) {
	ctx, cancel := context.WithTimeout(ctx, awakeCallTimeout)
	defer cancel()
	var fresh awake.Status
	if err := localCall(ctx, daemon.SocketPath(home), awake.MethodStatus, struct{}{}, &fresh); err != nil {
		return st, false
	}
	return fresh, true
}

func offlineStatus(s awake.Settings) awake.Status {
	return awake.Status{Mode: s.Mode, Reason: awake.ReasonIdle, Power: awake.PowerUnknown, BatteryFloor: s.BatteryFloor}
}

func showAwake(ctx context.Context, home string, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(ctx, awakeCallTimeout)
	defer cancel()
	var st awake.Status
	err := localCall(ctx, daemon.SocketPath(home), awake.MethodStatus, struct{}{}, &st)
	var derr *daemonError
	switch {
	case err == nil:
		printAwake(stdout, st, true)
	case errors.Is(err, errUnreachable) || errors.As(err, &derr) && derr.Code == rpc.CodeMethodNotFound:
		s, lerr := awake.LoadSettings(home)
		if lerr != nil {
			fmt.Fprintf(stderr, "togo: %v; using the defaults\n", lerr)
		}
		printAwake(stdout, offlineStatus(s), false)
	default:
		return fail(stderr, err)
	}
	return exitOK
}

var modeLines = map[awake.Mode]string{
	awake.ModeOff:     "never keeps this machine awake",
	awake.ModePlugged: "awake while a session is working or waiting on you; lid closed only on AC power",
	awake.ModeBattery: "awake while a session is working or waiting on you; lid closed on AC or battery",
}

func printAwake(w io.Writer, st awake.Status, live bool) {
	fmt.Fprintf(w, "Keep awake: %s (%s)\n", st.Mode, modeLines[st.Mode])
	if !live {
		fmt.Fprintln(w, "Now: not holding; the helper daemon is not running")
	} else {
		fmt.Fprintf(w, "Now: %s\n", awakeReason(st))
	}
	if live && st.PlatformSupported {
		power := map[string]string{awake.PowerAC: "AC", awake.PowerBattery: "battery", awake.PowerUnknown: "unknown"}[st.Power]
		if st.BatteryPercent != nil {
			power += fmt.Sprintf(", battery at %d%%", *st.BatteryPercent)
		}
		fmt.Fprintf(w, "Power: %s\n", power)
	}
	fmt.Fprintf(w, "Battery floor: %d%%\n", st.BatteryFloor)
	if live && st.NeedsAdmin && st.Mode != awake.ModeBattery && st.PlatformSupported {
		fmt.Fprintln(w, "To stay awake with the lid closed on battery: togo awake battery")
	}
}

func awakeReason(st awake.Status) string {
	switch st.Reason {
	case awake.ReasonUnsupported:
		return "keeping awake is not supported on this system"
	case awake.ReasonOff:
		return "not holding"
	case awake.ReasonIdle:
		return "not holding; no session is working or waiting on you"
	case awake.ReasonActive:
		return "holding; a session is working or waiting on you"
	case awake.ReasonGrace:
		return "holding for up to 2 minutes after the last session went quiet"
	case awake.ReasonOnBattery:
		return "holding, but on battery a closed lid still sleeps"
	case awake.ReasonNeedsAdmin:
		return "holding, but battery mode is not set up: a closed lid on battery still sleeps (run togo awake battery)"
	case awake.ReasonBatteryFloor:
		return fmt.Sprintf("holding, but lid-closed on battery is paused: battery at or below %d%%", st.BatteryFloor)
	case awake.ReasonThermal:
		return "holding, but lid-closed on battery is paused by a thermal warning"
	case awake.ReasonFailed:
		if st.Held {
			return "holding, but disabling sleep failed: " + st.Detail
		}
		return "not holding: " + st.Detail
	}
	return st.Reason
}

// awakeDoctorCheck is the doctor's awake line, from the daemon when one
// answers and from the saved settings otherwise.
func awakeDoctorCheck(ctx context.Context) doctor.Check {
	home, err := state.Home()
	if err != nil {
		return doctor.Check{Name: "awake", Status: doctor.StatusWarn, Detail: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var st awake.Status
	if err := localCall(ctx, daemon.SocketPath(home), awake.MethodStatus, struct{}{}, &st); err == nil {
		return doctor.AwakeCheck(st, true)
	}
	s, _ := awake.LoadSettings(home)
	return doctor.AwakeCheck(offlineStatus(s), false)
}

// uninstallAwake clears a disablesleep Togo set and removes the battery
// mode sudoers rule. sudo may ask for a password.
func uninstallAwake(ctx context.Context, home string, stdout, stderr io.Writer) int {
	if awakeGOOS != "darwin" {
		return exitOK
	}
	admin := awakeAdmin(home, stderr)
	removed, err := admin.Remove(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	if removed {
		fmt.Fprintf(stdout, "awake: removed %s\n", admin.SudoersPath)
	}
	return exitOK
}
