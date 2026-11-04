// Package doctor runs read-only checks that tell a person whether this host
// is ready for Togo: tmux, the Mosh and Eternal Terminal servers, the
// firewall and Tailscale SSH. It never changes system settings and never
// needs elevated rights.
package doctor

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
)

// Status is the outcome of one check.
type Status string

// The possible outcomes of a check.
const (
	StatusOK      Status = "ok"
	StatusMissing Status = "missing"
	StatusWarn    Status = "warn"
	StatusNote    Status = "note"
)

// Check is one line of the report.
type Check struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Detail  string `json:"detail"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Fix     string `json:"fix,omitempty"`
}

// Env holds everything the checks read from the machine, so tests can
// substitute it.
type Env struct {
	// Path is the login PATH, which is what a non-interactive ssh command
	// would see on a well-configured host.
	Path string
	Home string
	GOOS string
	// Fallbacks maps a binary name to absolute directories tried after Path.
	Fallbacks map[string][]string
	// Run executes bin with args and returns its combined output.
	Run func(ctx context.Context, bin string, args ...string) (string, error)
}

const (
	minTmuxMajor = 3
	minTmuxMinor = 2

	socketFilter = "/usr/libexec/ApplicationFirewall/socketfilterfw"
	runTimeout   = 3 * time.Second
)

// DefaultFallbacks returns the install directories tried after the login
// PATH. home is the user's home directory.
func DefaultFallbacks(home string) map[string][]string {
	nixUser := filepath.Join(home, ".nix-profile", "bin")
	var tmuxDirs []string
	for _, p := range session.TmuxFallbacks(home) {
		tmuxDirs = append(tmuxDirs, filepath.Dir(p))
	}
	return map[string][]string{
		"tmux": tmuxDirs,
		"mosh-server": {
			"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin",
			"/nix/var/nix/profiles/default/bin", nixUser,
		},
		"etserver": {"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"},
		"tailscale": {
			"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin",
			"/Applications/Tailscale.app/Contents/MacOS",
		},
	}
}

// DefaultEnv returns the production environment for the given login PATH.
func DefaultEnv(loginPath string) Env {
	home, _ := os.UserHomeDir()
	return Env{
		Path:      loginPath,
		Home:      home,
		GOOS:      runtime.GOOS,
		Fallbacks: DefaultFallbacks(home),
		Run:       execRun,
	}
}

func execRun(ctx context.Context, bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	return string(out), err
}

// Run performs every check in a fixed order: tmux, mosh-server, etserver,
// firewall, then tailscale when it applies.
func Run(ctx context.Context, e Env) []Check {
	checks := []Check{
		checkTmux(ctx, e),
		checkMosh(ctx, e),
		checkEternalTerminal(e),
		checkFirewall(ctx, e),
	}
	if c, ok := checkTailscale(ctx, e); ok {
		checks = append(checks, c)
	}
	return checks
}

// find looks for name in the absolute directories of the login PATH, then
// in the fallback directories. offPath reports a fallback hit.
func find(e Env, name string) (path string, offPath, ok bool) {
	for _, dir := range filepath.SplitList(e.Path) {
		if p := executableIn(dir, name); p != "" {
			return p, false, true
		}
	}
	for _, dir := range e.Fallbacks[name] {
		if p := executableIn(dir, name); p != "" {
			return p, true, true
		}
	}
	return "", false, false
}

func executableIn(dir, name string) string {
	if !filepath.IsAbs(dir) {
		return ""
	}
	p := filepath.Join(dir, name)
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return ""
	}
	return p
}

func installHint(e Env, pkg string) string {
	if e.GOOS == "darwin" {
		return "brew install " + pkg
	}
	return "sudo apt install " + pkg
}

var versionNum = regexp.MustCompile(`\d+\.\d+`)

func checkTmux(ctx context.Context, e Env) Check {
	c := Check{Name: "tmux"}
	path, _, ok := find(e, "tmux")
	if !ok {
		c.Status, c.Detail, c.Fix = StatusMissing, "tmux is not installed", installHint(e, "tmux")
		return c
	}
	c.Path = path
	out, _ := e.Run(ctx, path, "-V")
	line := firstLine(out)
	m := versionNum.FindString(line)
	if m == "" {
		c.Status, c.Detail = StatusWarn, "could not read the tmux version"
		return c
	}
	c.Version = m
	major, minor := splitVersion(m)
	if major < minTmuxMajor || major == minTmuxMajor && minor < minTmuxMinor {
		c.Status = StatusWarn
		c.Detail = fmt.Sprintf("tmux %s is too old; Togo needs 3.2 or newer", m)
		c.Fix = "Upgrade tmux to 3.2 or newer"
		return c
	}
	c.Status, c.Detail = StatusOK, "tmux "+m
	return c
}

func splitVersion(v string) (major, minor int) {
	a, b, _ := strings.Cut(v, ".")
	major, _ = strconv.Atoi(a)
	minor, _ = strconv.Atoi(b)
	return major, minor
}

func checkMosh(ctx context.Context, e Env) Check {
	c := Check{Name: "mosh-server"}
	path, offPath, ok := find(e, "mosh-server")
	if !ok {
		c.Status, c.Detail, c.Fix = StatusMissing, "mosh-server is not installed", installHint(e, "mosh")
		return c
	}
	c.Path = path
	out, _ := e.Run(ctx, path, "--version")
	c.Version = firstLine(out)
	c.Status, c.Detail = StatusOK, cmp.Or(c.Version, "mosh-server found")
	if offPath {
		c.Detail += "; found outside the login PATH; Togo finds it, other Mosh clients may not"
	}
	return c
}

func checkEternalTerminal(e Env) Check {
	if path, _, ok := find(e, "etserver"); ok {
		return Check{Name: "etserver", Status: StatusOK, Detail: "etserver found", Path: path}
	}
	return Check{
		Name:   "etserver",
		Status: StatusNote,
		Detail: "Optional. Togo offers Eternal Terminal only on hosts with etserver",
	}
}

func checkFirewall(ctx context.Context, e Env) Check {
	c := Check{Name: "firewall"}
	if e.GOOS != "darwin" {
		c.Status, c.Detail = StatusNote, "If a firewall is on, open UDP 60000-61000 for Mosh"
		return c
	}
	out, _ := e.Run(ctx, socketFilter, "--getglobalstate")
	if strings.Contains(out, "enabled") {
		c.Status = StatusWarn
		c.Detail = "macOS firewall is on: allow mosh-server to receive UDP 60000-61000"
		return c
	}
	c.Status, c.Detail = StatusOK, "macOS firewall is off"
	return c
}

func checkTailscale(ctx context.Context, e Env) (Check, bool) {
	path, _, ok := find(e, "tailscale")
	if !ok {
		return Check{}, false
	}
	out, err := e.Run(ctx, path, "debug", "prefs")
	if err != nil {
		return Check{}, false
	}
	var prefs struct {
		RunSSH bool
	}
	if json.Unmarshal([]byte(out), &prefs) != nil || !prefs.RunSSH {
		return Check{}, false
	}
	return Check{
		Name:   "tailscale",
		Status: StatusNote,
		Detail: "Tailscale SSH is on and can stall key logins; run tailscale set --ssh=false to use this machine's sshd",
		Path:   path,
	}, true
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
