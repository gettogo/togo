package awake

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

// SleepOverride turns macOS's system-wide disablesleep on and off.
type SleepOverride interface {
	Set(ctx context.Context, on bool) error
}

// Runner runs argv (an absolute path and its arguments, never a shell).
type Runner func(ctx context.Context, argv ...string) error

const (
	sudoPath   = "/usr/bin/sudo"
	visudoPath = "/usr/sbin/visudo"
	teePath    = "/usr/bin/tee"
	chownPath  = "/usr/sbin/chown"
	chmodPath  = "/bin/chmod"
	mvPath     = "/bin/mv"
	rmPath     = "/bin/rm"

	// DefaultSudoersPath is where `togo awake battery` installs its rule.
	DefaultSudoersPath = "/etc/sudoers.d/togo-awake"
)

// SudoPmset sets disablesleep with `sudo -n`, which fails rather than asks
// for a password, so the daemon only succeeds when the sudoers rule exists.
type SudoPmset struct {
	// Run runs argv. Nil runs it with no terminal and a timeout.
	Run Runner
}

// Set implements SleepOverride. It touches no pmset setting but
// disablesleep.
func (s SudoPmset) Set(ctx context.Context, on bool) error {
	run := s.Run
	if run == nil {
		run = quietRun
	}
	v := "0"
	if on {
		v = "1"
	}
	return run(ctx, sudoPath, "-n", pmsetPath, "-a", "disablesleep", v)
}

func quietRun(ctx context.Context, argv ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err != nil && len(out) > 0 {
		return fmt.Errorf("%w: %s", err, firstLine(string(out)))
	}
	return err
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

// ClearOverride sets disablesleep back to 0 if Togo set it (the marker
// exists), and removes the marker. It reports whether it cleared anything.
// A failed clear keeps the marker so a later attempt tries again.
func ClearOverride(ctx context.Context, home string, o SleepOverride) (bool, error) {
	if !overrideMarked(home) {
		return false, nil
	}
	if err := o.Set(ctx, false); err != nil {
		return false, err
	}
	return true, unmarkOverride(home)
}

// userRE is a conservative account name: nothing a sudoers parser could
// read as a list, group or uid. aliasRE matches what it would read as an
// alias (or ALL) instead of a user.
var (
	userRE  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,31}$`)
	aliasRE = regexp.MustCompile(`^[A-Z0-9_]+$`)
)

// SudoersRule is the whole drop-in for user: two exact pmset commands and
// nothing else.
func SudoersRule(user string) (string, error) {
	if !userRE.MatchString(user) || aliasRE.MatchString(user) || user == "root" {
		return "", fmt.Errorf("cannot write a sudoers rule for user %q", user)
	}
	return "# Lets togo turn sleep off and on for \"togo awake battery\".\n" +
		"# Remove with: togo awake plugged\n" +
		user + " ALL=(root) NOPASSWD: " + pmsetPath + " -a disablesleep 0, " + pmsetPath + " -a disablesleep 1\n", nil
}

// Admin performs the one-time battery mode setup and its removal. Its
// sudo calls may ask for a password on the terminal.
type Admin struct {
	// Home is TOGO_HOME, where the override marker lives.
	Home        string
	User        string
	SudoersPath string
	// Run runs argv attached to the user's terminal.
	Run Runner
	// Feed runs argv like Run with input on its standard input. sudo
	// reads a password from the terminal, not from standard input.
	Feed func(ctx context.Context, input string, argv ...string) error
}

// Installed reports whether the sudoers rule exists.
func (a Admin) Installed() bool {
	_, err := os.Stat(a.SudoersPath)
	return err == nil
}

// Marked reports whether disablesleep is set by Togo.
func (a Admin) Marked() bool { return overrideMarked(a.Home) }

// stagedPath is where the rule is checked before it is renamed into
// place. sudo skips files in sudoers.d whose names contain a dot, so a
// staged rule is never read.
func (a Admin) stagedPath() string {
	return filepath.Join(filepath.Dir(a.SudoersPath), "."+filepath.Base(a.SudoersPath)+".tmp")
}

// Enable installs the sudoers rule, then proves it works unattended by
// setting disablesleep with sudo -n, marking it as Togo's so the daemon
// clears it when nothing is active.
//
// The rule is written by root from this process's memory into a staged
// file in the root-owned sudoers.d, checked there by visudo and renamed
// into place, so nothing the user's other processes can write is ever
// installed and sudo never reads a partial file. A failure removes the
// staged file; a failure after disablesleep may have been set puts it
// back to 0 and removes the rule.
func (a Admin) Enable(ctx context.Context) error {
	rule, err := SudoersRule(a.User)
	if err != nil {
		return err
	}
	staged := a.stagedPath()
	steps := [][]string{
		{sudoPath, chownPath, "root:wheel", staged},
		{sudoPath, chmodPath, "0440", staged},
		{sudoPath, visudoPath, "-cf", staged},
		{sudoPath, mvPath, "-f", staged, a.SudoersPath},
	}
	if err := a.Feed(ctx, rule, sudoPath, teePath, staged); err != nil {
		_ = a.Run(ctx, sudoPath, rmPath, "-f", staged)
		return fmt.Errorf("write %s: %w", staged, err)
	}
	for _, argv := range steps {
		if err := a.Run(ctx, argv...); err != nil {
			_ = a.Run(ctx, sudoPath, rmPath, "-f", staged)
			return fmt.Errorf("install %s: %s: %w", a.SudoersPath, filepath.Base(argv[1]), err)
		}
	}
	if err := markOverride(a.Home); err != nil {
		_ = a.Run(ctx, sudoPath, rmPath, "-f", a.SudoersPath)
		return err
	}
	if err := a.Run(ctx, sudoPath, "-n", pmsetPath, "-a", "disablesleep", "1"); err != nil {
		rerr := a.Restore(ctx)
		_ = a.Run(ctx, sudoPath, rmPath, "-f", a.SudoersPath)
		return errors.Join(fmt.Errorf("the installed rule does not let togo run pmset: %w", err), rerr)
	}
	return nil
}

// Restore sets disablesleep back to 0 through a plain sudo if Togo set
// it, and removes the marker.
func (a Admin) Restore(ctx context.Context) error {
	if !overrideMarked(a.Home) {
		return nil
	}
	if err := a.Run(ctx, sudoPath, pmsetPath, "-a", "disablesleep", "0"); err != nil {
		return fmt.Errorf("set disablesleep back to 0: %w", err)
	}
	return unmarkOverride(a.Home)
}

// Remove sets disablesleep back to 0 if Togo set it, then deletes the
// sudoers rule if it exists, and reports whether it deleted the rule. The
// clear runs first and through a plain sudo, so a daemon that could not
// clear it never leaves sleep disabled with no rule left to undo it.
func (a Admin) Remove(ctx context.Context) (bool, error) {
	if err := a.Restore(ctx); err != nil {
		return false, err
	}
	if !a.Installed() {
		return false, nil
	}
	if err := a.Run(ctx, sudoPath, rmPath, "-f", a.SudoersPath); err != nil {
		return false, fmt.Errorf("remove %s: %w", a.SudoersPath, err)
	}
	return true, nil
}
