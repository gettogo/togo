package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strings"
	"time"
)

// DefaultManager is the per-user service manager of this OS.
const DefaultManager = Systemd

const loginctl = "/usr/bin/loginctl"

var userNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_.-]{0,31}$`)

// Linger reads and sets systemd's linger flag, which keeps a user's
// service manager running after the last login ends.
type Linger interface {
	Enabled(user string) (bool, error)
	Enable(user string) error
}

// systemLinger drives loginctl. run is replaceable so tests never reach the
// real one.
type systemLinger struct {
	run func(argv []string) (string, error)
}

func (l systemLinger) exec(argv []string) (string, error) {
	if l.run != nil {
		return l.run(argv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	return string(out), err
}

func (l systemLinger) Enabled(name string) (bool, error) {
	if !userNamePattern.MatchString(name) {
		return false, fmt.Errorf("refusing user name %q", name)
	}
	out, err := l.exec([]string{loginctl, "show-user", name, "-p", "Linger", "--value"})
	if err != nil {
		return false, fmt.Errorf("loginctl show-user: %w", err)
	}
	return strings.TrimSpace(out) == "yes", nil
}

func (l systemLinger) Enable(name string) error {
	if !userNamePattern.MatchString(name) {
		return fmt.Errorf("refusing user name %q", name)
	}
	if out, err := l.exec([]string{loginctl, "enable-linger", name}); err != nil {
		return fmt.Errorf("loginctl enable-linger: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// Tests replace these.
var (
	lingerControl Linger    = systemLinger{}
	lingerNotice  io.Writer = os.Stderr
	currentUser             = func() (string, error) {
		u, err := user.Current()
		if err != nil {
			return "", err
		}
		return u.Username, nil
	}
)

// afterInstall turns on linger so the daemon outlives the last SSH login.
// Failing to do so is not an install failure: it prints the command the
// person can run with sudo.
func (s Service) afterInstall(context.Context) {
	name, err := currentUser()
	if err != nil || !userNamePattern.MatchString(name) {
		fmt.Fprintln(lingerNotice, "Togo stops when you log out. To keep it running, run: sudo loginctl enable-linger $USER")
		return
	}
	if on, err := lingerControl.Enabled(name); err == nil && on {
		return
	}
	if err := lingerControl.Enable(name); err != nil {
		fmt.Fprintf(lingerNotice, "Togo stops when you log out. To keep it running, run: sudo loginctl enable-linger %s\n", name)
	}
}
