// Package doctor runs read-only checks that tell a person whether this host
// is ready for Togo: tmux, the Mosh and Eternal Terminal servers, the
// firewall and Tailscale SSH. It never changes system settings and never
// needs elevated rights.
package doctor

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/hostinfo"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/tailnet"
	"github.com/gettogo/togo/helper/internal/wire"
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

// Failed reports whether c is a required check that failed: Togo cannot
// work without tmux 3.2 or newer. Every other finding is advice. A tmux
// whose version could not be read is a warning, not a failure.
func Failed(c Check) bool {
	return c.Name == "tmux" && (c.Status == StatusMissing || c.Status == StatusWarn && c.Version != "")
}

// AnyFailed reports whether any check in checks is a required one that
// failed, wherever it sits in the report.
func AnyFailed(checks []Check) bool {
	for _, c := range checks {
		if Failed(c) {
			return true
		}
	}
	return false
}

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
	// Awake reports how the machine is kept awake; nil leaves the line out.
	Awake func(ctx context.Context) Check
	// TailscaleApp is the app bundle's tailscale binary, tried after Path and
	// Fallbacks; empty skips it.
	TailscaleApp string
	// Routes lists the addresses host.info reports; nil leaves the line out.
	Routes func(ctx context.Context) []wire.HostRoute
	// Login and User feed the linger line on Linux; a nil Login leaves it out.
	Login LoginInfo
	User  string
	// SSHListening reports whether sshd answers on this machine (Remote
	// Login on macOS); nil leaves the line out.
	SSHListening func(ctx context.Context) bool
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
	fb := map[string][]string{
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
	for _, a := range agents.Registry {
		if a.Command != "" {
			fb[a.Command] = agents.FallbackDirs(home)
		}
	}
	return fb
}

// DefaultEnv returns the production environment for the given login PATH.
func DefaultEnv(loginPath string) Env {
	home, _ := os.UserHomeDir()
	e := &Env{
		Path:      loginPath,
		Home:      home,
		GOOS:      runtime.GOOS,
		Fallbacks: DefaultFallbacks(home),
		Run:       execRun,

		TailscaleApp: tailnet.AppBinary,
		Routes:       hostinfo.SystemRoutes,
		SSHListening: sshListening,
	}
	if e.GOOS == "linux" {
		e.Login, e.User = defaultLogin(e)
	}
	return *e
}

func execRun(ctx context.Context, bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	return string(out), err
}

// Run performs every check in a fixed order: tmux, mosh-server, etserver,
// agents, firewall, tailscale, routes, then awake and linger.
func Run(ctx context.Context, e Env) []Check {
	var checks []Check
	if e.SSHListening != nil {
		checks = append(checks, checkRemoteLogin(ctx, e))
	}
	checks = append(checks,
		checkTmux(ctx, e),
		checkMosh(ctx, e),
		checkEternalTerminal(e),
	)
	checks = append(checks, checkAgents(e)...)
	checks = append(checks, checkFirewall(ctx, e))
	checks = append(checks, checkTailscale(ctx, e)...)
	if e.Routes != nil {
		checks = append(checks, checkRoutes(e.Routes(ctx)))
	}
	if e.Awake != nil {
		checks = append(checks, e.Awake(ctx))
	}
	if e.GOOS == "linux" && e.Login != nil {
		checks = append(checks, LingerCheck(e))
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

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

// checkAgents reports where each coding agent resolves. A missing agent is
// a note, not a failure: nobody needs all of them.
func checkAgents(e Env) []Check {
	var checks []Check
	for _, a := range agents.Registry {
		if a.Command == "" {
			continue
		}
		path, offPath, ok := find(e, a.Command)
		if !ok {
			checks = append(checks, Check{Name: a.Command, Status: StatusNote, Detail: a.Name + " not found on the login PATH or in common install folders"})
			continue
		}
		c := Check{Name: a.Command, Status: StatusOK, Detail: a.Name + " found", Path: path}
		if offPath {
			c.Detail += "; found outside the login PATH; Togo launches it by full path"
		}
		checks = append(checks, c)
	}
	return checks
}
