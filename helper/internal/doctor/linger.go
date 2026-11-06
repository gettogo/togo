package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Result is one doctor line.
type Result = Check

// LoginInfo reads how systemd treats this user's logins.
type LoginInfo interface {
	// Linger reports whether the user's service manager outlives logins.
	Linger(user string) (bool, error)
	// KillUserProcesses reports whether logind ends a user's processes when
	// the last session closes.
	KillUserProcesses() (bool, error)
	// TmuxCgroup is the cgroup of the Togo tmux server.
	TmuxCgroup() (string, error)
}

const (
	lingerFix = "Togo stops when you log out. To keep it running, run: sudo loginctl enable-linger %s"
	scopeFix  = "Sessions started from a login shell stop when you log out. Start them from the phone or after togo service install."
)

var pidDigits = regexp.MustCompile(`^[0-9]+$`)

var loginName = regexp.MustCompile(`^[a-z_][a-z0-9_.-]{0,31}$`)

// LingerCheck reports whether the helper keeps running after the last login
// ends. It returns a line named "linger"; callers leave it out off Linux.
func LingerCheck(env Env) Result {
	c := Check{Name: "linger"}
	name := env.User
	if !loginName.MatchString(name) {
		c.Status, c.Detail = StatusNote, "could not read the user name"
		return c
	}
	on, err := env.Login.Linger(name)
	switch {
	case err != nil:
		c.Status, c.Detail = StatusNote, "could not read the linger setting"
	case !on:
		c.Status, c.Detail, c.Fix = StatusWarn, "off", fmt.Sprintf(lingerFix, name)
	default:
		c.Status, c.Detail = StatusOK, "on"
		if kill, err := env.Login.KillUserProcesses(); err == nil && kill {
			if cg, err := env.Login.TmuxCgroup(); err == nil && strings.Contains(cg, "session-") && strings.Contains(cg, ".scope") {
				c.Status, c.Fix = StatusWarn, scopeFix
			}
		}
	}
	return c
}

// systemLogin is the production LoginInfo.
type systemLogin struct {
	tmux func() string
}

func (systemLogin) Linger(name string) (bool, error) {
	if !loginName.MatchString(name) {
		return false, fmt.Errorf("refusing user name %q", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/loginctl", "show-user", name, "-p", "Linger", "--value").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "yes", nil
}

// KillUserProcesses reads logind's configuration from the real roots.
func (systemLogin) KillUserProcesses() (bool, error) {
	return killUserProcesses("/"), nil
}

// killUserProcesses applies systemd's precedence under root: the vendor
// file, then the admin file, then drop-ins from /usr/lib, /run and /etc
// (a name in /etc masks the same name in /run, which masks /usr/lib), in
// lexical order. The last assignment in a [Login] section wins.
func killUserProcesses(root string) bool {
	rd := func(p string) string {
		b, _ := os.ReadFile(filepath.Join(root, p))
		return string(b)
	}
	kill := false
	apply := func(text string) {
		if v, ok := loginKillSetting(text); ok {
			kill = v
		}
	}
	apply(rd("usr/lib/systemd/logind.conf"))
	apply(rd("etc/systemd/logind.conf"))
	dirs := []string{"usr/lib/systemd/logind.conf.d", "run/systemd/logind.conf.d", "etc/systemd/logind.conf.d"}
	chosen := map[string]string{}
	for _, d := range dirs {
		names, _ := filepath.Glob(filepath.Join(root, d, "*.conf"))
		for _, n := range names {
			chosen[filepath.Base(n)] = filepath.Join(d, filepath.Base(n))
		}
	}
	order := make([]string, 0, len(chosen))
	for n := range chosen {
		order = append(order, n)
	}
	slices.Sort(order)
	for _, n := range order {
		apply(rd(chosen[n]))
	}
	return kill
}

// loginKillSetting returns the last KillUserProcesses value inside [Login].
func loginKillSetting(text string) (value, found bool) {
	inLogin := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
		case line[0] == '[':
			inLogin = line == "[Login]"
		case inLogin:
			k, v, ok := strings.Cut(line, "=")
			if !ok || strings.TrimSpace(k) != "KillUserProcesses" {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "yes", "true", "1", "on":
				value = true
			default:
				value = false
			}
			found = true
		}
	}
	return value, found
}

func (l systemLogin) TmuxCgroup() (string, error) {
	tmux := ""
	if l.tmux != nil {
		tmux = l.tmux()
	}
	if tmux == "" {
		return "", fmt.Errorf("tmux not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, tmux, "-L", "togo", "display", "-p", "#{pid}").Output()
	if err != nil {
		return "", err
	}
	pid := strings.TrimSpace(string(out))
	if !pidDigits.MatchString(pid) {
		return "", fmt.Errorf("unexpected tmux pid %q", pid)
	}
	b, err := os.ReadFile("/proc/" + pid + "/cgroup")
	return string(b), err
}

func defaultLogin(e *Env) (LoginInfo, string) {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	return systemLogin{tmux: func() string {
		p, _, _ := find(*e, "tmux")
		return p
	}}, name
}
