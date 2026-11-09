package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gettogo/togo/helper/internal/authkeys"
	"github.com/gettogo/togo/helper/internal/push"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/wire"
)

func init() {
	register("devices", func(_ context.Context, args []string, env map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		return runDevices(args, env, stdout, stderr)
	})
	register("push", func(ctx context.Context, args []string, env map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		return runPush(ctx, args, env, stdout, stderr)
	})
	addUsage("  devices ls | revoke <deviceId>                list or unpair phones that get pushes")
	addUsage("  push test                                     send a test notification to every phone")
}

// pushService opens the pairing store under $TOGO_HOME from env, falling
// back to the default home.
func pushService(env map[string]string) (*push.Service, error) {
	home := env["TOGO_HOME"]
	if home == "" {
		var err error
		if home, err = state.Home(); err != nil {
			return nil, err
		}
	}
	return &push.Service{Home: home, Getenv: func(k string) string { return env[k] }}, nil
}

func runDevices(args []string, env map[string]string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr, errors.New("devices takes ls or revoke"))
	}
	svc, err := pushService(env)
	if err != nil {
		return fail(stderr, err)
	}
	switch {
	case args[0] == "ls" && len(args) == 1:
		list, err := svc.Devices().List()
		if err != nil {
			return fail(stderr, err)
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "no paired devices")
			return exitOK
		}
		// Tokens and keys stay in the store; only what identifies a phone
		// to its owner is printed.
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tDEVICE\tPLATFORM\tENV\tLIVE\tCOMMAND\tPAIRED\tSTATUS")
		for _, d := range list {
			status := "active"
			if d.Dead {
				status = "unregistered"
			}
			// Live Activities run on iOS only, and the command opt-in
			// means nothing without them.
			live, command := "-", "-"
			if d.EffectivePlatform() == wire.PlatformIOS {
				live, command = onOff(d.LiveActivityToken != "" || d.LiveActivityStartToken != ""), onOff(d.LiveActivityCommand)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", d.Name, d.DeviceID, d.EffectivePlatform(), d.Env,
				live, command, d.PairedAt.Local().Format(time.DateOnly), status)
		}
		if err := tw.Flush(); err != nil {
			return fail(stderr, err)
		}
		return exitOK
	case args[0] == "revoke" && len(args) == 2:
		list, err := svc.Devices().List()
		if err != nil {
			return fail(stderr, err)
		}
		var dev push.Device
		for _, d := range list {
			if d.DeviceID == args[1] {
				dev = d
			}
		}
		if err := svc.Devices().Remove(args[1]); err != nil {
			if errors.Is(err, push.ErrUnknownDevice) {
				return fail(stderr, fmt.Errorf("no paired device %q", args[1]))
			}
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "revoked %s\n", args[1])
		removeDeviceKey(env, dev, stdout, stderr)
		return exitOK
	}
	return usageError(stderr, errors.New("devices takes ls or revoke <deviceId>"))
}

// removeDeviceKey takes a revoked device's key out of
// ~/.ssh/authorized_keys: the lines holding exactly the key it sent at
// pairing under a Togo comment. The device is already unpaired, so a key
// that cannot be removed is reported for the user to remove by hand rather
// than failing.
func removeDeviceKey(env map[string]string, dev push.Device, stdout, stderr io.Writer) {
	path, err := authorizedKeysPath(env)
	if err != nil {
		fmt.Fprintf(stderr, "togo: could not remove this device's key from ~/.ssh/authorized_keys: %v\n", err)
		return
	}
	if dev.SSHKey == "" {
		reportUnknownKey(path, stdout)
		return
	}
	n, err := authkeys.Remove(path, dev.SSHKey)
	switch {
	case err == nil && n > 0:
		fmt.Fprintf(stdout, "Removed this device's key from ~/.ssh/authorized_keys (previous copy in ~/.ssh/authorized_keys%s).\n", authkeys.BackupSuffix)
		return
	case err == nil:
		reportKeyNotFound(path, dev.SSHKey, stdout)
		return
	}
	var c *authkeys.Conflict
	if !errors.As(err, &c) {
		fmt.Fprintf(stderr, "togo: could not remove this device's key from ~/.ssh/authorized_keys: %v\n", err)
		return
	}
	byHand := "; remove the line containing " + keyHint(dev.SSHKey) + " by hand."
	switch c.Reason {
	case authkeys.ReasonSymlink:
		fmt.Fprintln(stdout, "~/.ssh/authorized_keys is a link, so this device's key was not removed"+byHand)
	case authkeys.ReasonNotRegular:
		fmt.Fprintln(stdout, "~/.ssh/authorized_keys is not a regular file, so this device's key was not removed"+byHand)
	default:
		fmt.Fprintln(stdout, "~/.ssh/authorized_keys does not exist, so there was no key to remove.")
	}
}

// reportKeyNotFound explains why no line was removed: the key is absent,
// or present only under a comment Togo did not write, which is left alone.
func reportKeyNotFound(path, key string, stdout io.Writer) {
	lines, _ := authkeys.Lines(path)
	var foreign int
	for _, l := range lines {
		if l.Key() == key {
			foreign++
		}
	}
	if foreign == 0 {
		fmt.Fprintln(stdout, "No key for this device was found in ~/.ssh/authorized_keys.")
		return
	}
	fmt.Fprintf(stdout, "This device's key (%s) is in ~/.ssh/authorized_keys under a comment Togo did not write, "+
		"so it was left in place; remove it by hand if this device should lose SSH access.\n", keyHint(key))
}

// reportUnknownKey covers a device that paired without sending its key:
// which line is its own cannot be told apart from another device's, so
// nothing is removed and the Togo lines are listed for the user.
func reportUnknownKey(path string, stdout io.Writer) {
	lines, err := authkeys.Lines(path)
	if err != nil {
		return
	}
	var togo []authkeys.Line
	for _, l := range lines {
		// A line that is not a well-formed key is never printed: its
		// bytes come from the file, not from Togo.
		if _, _, err := authkeys.ParseKey(l.Key()); err == nil && authkeys.IsTogoComment(l.Comment) {
			togo = append(togo, l)
		}
	}
	if len(togo) == 0 {
		return
	}
	fmt.Fprintln(stdout, "This device did not send its SSH key when it paired, so Togo cannot tell which line in "+
		"~/.ssh/authorized_keys is its own and removed none. Togo key lines there:")
	for _, l := range togo {
		fmt.Fprintf(stdout, "  %s %s\n", keyHint(l.Key()), strconv.Quote(l.Comment))
	}
	fmt.Fprintln(stdout, "Remove this device's line by hand so it loses SSH access.")
}

// authorizedKeysPath is ~/.ssh/authorized_keys under HOME from env, so
// tests and the CLI agree on the home they mean.
func authorizedKeysPath(env map[string]string) (string, error) {
	if home := env["HOME"]; home != "" {
		return filepath.Join(home, ".ssh", "authorized_keys"), nil
	}
	return authkeys.DefaultPath()
}

// keyHint names a key line by its type and the end of its blob. The
// start of a blob only repeats the type, so the end is what tells two keys
// apart. The comment is not used: the phone writes its own hostname there,
// which the pairing store does not know.
func keyHint(key string) string {
	keyType, blob, _ := strings.Cut(key, " ")
	if len(blob) > keyHintLen {
		blob = "…" + blob[len(blob)-keyHintLen:]
	}
	return keyType + " " + blob
}

const keyHintLen = 16

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func runPush(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "test" {
		return usageError(stderr, errors.New("push takes test"))
	}
	svc, err := pushService(env)
	if err != nil {
		return fail(stderr, err)
	}
	s := &push.Sender{Keys: svc.HostKeys, Devices: svc.Devices()}
	results := s.Deliver(ctx, push.Message{
		Kind: "alert",
		Plaintext: wire.PushPlaintext{
			V: 1, Kind: wire.PushNotify, Title: "Togo", Body: "Test notification from this host",
			TS: time.Now().UnixMilli(),
		},
	})
	if len(results) == 0 {
		fmt.Fprintln(stdout, "no active paired devices")
		return exitOK
	}
	code := exitOK
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(stdout, "%s: failed: %v\n", r.DeviceID, r.Err)
			code = exitError
			continue
		}
		fmt.Fprintf(stdout, "%s: sent\n", r.DeviceID)
	}
	return code
}
