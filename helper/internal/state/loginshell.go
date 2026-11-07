package state

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The user's login shell is read once for both PATH and the environment
// agents run in ("$SHELL -lic", as agents are launched). Reading it can
// hang: a .zprofile line that sources completion scripts has been seen
// stuck for good under launchd. So the read never holds anyone up:
//
//   - it runs in the background, detached from any terminal, stdin
//     /dev/null, TERM set, killed as a process group at loginPathTimeout;
//   - callers get the last good read at once: this process's, or the one
//     kept on disk from an earlier run (PersistLoginShell);
//   - with nothing read yet, a caller waits at most loginFirstWait (or its
//     own context) and then gets the process PATH and no environment;
//   - a failed read is retried in the background, backing off to
//     loginRetryMax, and a good one is refreshed after loginRefresh.

// loginPathTimeout bounds one shell read. A variable so tests can shorten
// it.
var loginPathTimeout = 5 * time.Second

// loginPathRetry is how long after a failed read the next caller may start
// another; background retries back off from it.
const loginPathRetry = 30 * time.Second

// loginRetryMax caps the pause between background retries.
const loginRetryMax = 10 * time.Minute

// loginRefresh is how old a good read may get before a caller starts a
// background read to refresh it (a new install, a new key in .zshrc).
const loginRefresh = 15 * time.Minute

// loginFirstWait is how long a caller with no read at all waits for one in
// flight.
var loginFirstWait = 2 * time.Second

// loginPathNow is a test seam for the retry window.
var loginPathNow = time.Now

// retryLater schedules a background retry; tests replace it.
var retryLater = func(d time.Duration, f func()) { time.AfterFunc(d, f) }

// loginEnvMarker frames the environment in the shell's output, so text
// that login and rc files print (a motd, an echo in .zshrc) is not
// mistaken for it.
const loginEnvMarker = "\x1eTOGO_ENV\x1e"

// loginCacheFile holds the last good read under TOGO_HOME. It holds what
// the user's shell exports, keys included, so it is written 0600.
const loginCacheFile = "login-shell.json"

// skipLoginEnv are variables that describe the shell that printed the
// environment, not the user's setup: a program the helper runs gets its
// own.
var skipLoginEnv = map[string]bool{"PWD": true, "OLDPWD": true, "SHLVL": true, "_": true}

type loginShell struct {
	mu       sync.Mutex
	env      []string  // last good read; nil when there is none
	at       time.Time // when env was read
	fresh    bool      // env was read by this process, not loaded from disk
	cache    string    // file the last good read is kept in; "" keeps none
	loaded   bool      // the cache file was looked at
	running  chan struct{}
	failedAt time.Time
	failures int
}

var login = &loginShell{}

// PersistLoginShell keeps the last good login shell read in dir, so a
// helper that starts while the shell hangs still has the user's PATH and
// keys. The daemon calls it once at start; without it reads stay in memory.
func PersistLoginShell(dir string) {
	login.mu.Lock()
	defer login.mu.Unlock()
	login.cache, login.loaded = "", false
	if dir != "" {
		login.cache = filepath.Join(dir, loginCacheFile)
	}
	if login.fresh {
		login.saveLocked() // read before this call
	}
}

// LoginPath returns PATH as the user's interactive login shell sets it, the
// same "$SHELL -lic" agents are launched with. The helper may run under
// launchd or a bare ssh command with a minimal PATH, so agent and tmux
// lookups use this instead. Without any good read it is the process PATH.
func LoginPath(ctx context.Context) string {
	p, _ := LoginPathSource(ctx)
	return p
}

// LoginPathSource is LoginPath that also reports whether the PATH came from
// the login shell (true) or is the process PATH fallback (false).
func LoginPathSource(ctx context.Context) (path string, fromShell bool) {
	env, _ := login.get(ctx)
	if p := envValue(env, "PATH"); p != "" {
		return p, true
	}
	return os.Getenv("PATH"), false
}

// LoginEnv returns the environment the user's interactive login shell sets
// up, the one agents run in. Harness programs the helper runs itself (a
// model list, a provider list) read their API keys from it: a key exported
// in .zshrc is in the agent's environment but not in a helper started by
// launchd. Nil when the shell was never read, so callers keep the process
// environment.
func LoginEnv(ctx context.Context) []string {
	env, _ := login.get(ctx)
	return env
}

// WarmLoginPath reads the login shell in the background so the first agent
// lookup does not wait for it, and reports which source won.
func WarmLoginPath(ctx context.Context, logf func(format string, args ...any)) {
	go func() {
		_, fromShell := LoginPathSource(ctx)
		if logf == nil {
			return
		}
		if fromShell {
			logf("login PATH read from the login shell")
		} else {
			logf("login shell gave no PATH in time; using the process PATH and common install folders, will retry")
		}
	}()
}

// get returns the last good read, starting a background read when there
// is none or it is due. With none at all it waits for the read a little.
func (l *loginShell) get(ctx context.Context) ([]string, bool) {
	l.mu.Lock()
	l.loadLocked()
	if l.env != nil {
		env := l.env
		if l.dueLocked() {
			l.startLocked()
		}
		l.mu.Unlock()
		return env, true
	}
	done := l.startLocked()
	l.mu.Unlock()
	if done == nil {
		return nil, false
	}
	wait := time.NewTimer(loginFirstWait)
	defer wait.Stop()
	select {
	case <-done:
	case <-ctx.Done():
	case <-wait.C:
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.env, l.env != nil
}

// dueLocked reports whether a held read should be refreshed: it came from
// disk, or it is old, and no read is running or recently failed.
func (l *loginShell) dueLocked() bool {
	if l.running != nil || l.inRetryLocked() {
		return false
	}
	return !l.fresh || loginPathNow().Sub(l.at) > loginRefresh
}

func (l *loginShell) inRetryLocked() bool {
	return !l.failedAt.IsZero() && loginPathNow().Sub(l.failedAt) < loginPathRetry
}

// startLocked starts a read unless one runs or one just failed. It returns
// the running read's done channel, or nil.
func (l *loginShell) startLocked() chan struct{} {
	if l.running != nil {
		return l.running
	}
	if l.inRetryLocked() {
		return nil
	}
	ch := make(chan struct{})
	l.running = ch
	go l.read(ch)
	return ch
}

// read runs the shell once. Its context is its own: a caller giving up
// must not cut a read others will use.
func (l *loginShell) read(done chan struct{}) {
	defer close(done)
	env, ok := loginEnv(context.Background(), cmp.Or(os.Getenv("SHELL"), "/bin/sh"))
	l.mu.Lock()
	defer l.mu.Unlock()
	l.running = nil
	if ok {
		l.env, l.at, l.fresh = env, loginPathNow(), true
		l.failedAt, l.failures = time.Time{}, 0
		l.saveLocked()
		return
	}
	l.failedAt = loginPathNow()
	l.failures++
	retryLater(retryDelay(l.failures), l.retry)
}

// retry is a background retry after a failed read.
func (l *loginShell) retry() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fresh {
		return
	}
	l.failedAt = time.Time{}
	l.startLocked()
}

// retryDelay doubles from loginPathRetry up to loginRetryMax.
func retryDelay(failures int) time.Duration {
	d := loginPathRetry
	for i := 1; i < failures && d < loginRetryMax; i++ {
		d *= 2
	}
	return min(d, loginRetryMax)
}

type loginCache struct {
	At  time.Time `json:"at"`
	Env []string  `json:"env"`
}

func (l *loginShell) loadLocked() {
	if l.loaded || l.cache == "" {
		return
	}
	l.loaded = true
	if l.env != nil {
		return
	}
	b, err := os.ReadFile(l.cache)
	if err != nil {
		return
	}
	var c loginCache
	if json.Unmarshal(b, &c) != nil || envValue(c.Env, "PATH") == "" {
		return
	}
	l.env, l.at, l.fresh = c.Env, c.At, false
}

func (l *loginShell) saveLocked() {
	if l.cache == "" {
		return
	}
	b, err := json.Marshal(loginCache{At: l.at.UTC(), Env: l.env})
	if err == nil {
		_ = WriteFile(l.cache, b)
	}
}

// loginPath asks shell for its interactive login PATH, falling back to the
// process PATH. fromShell is false for the fallback.
func loginPath(ctx context.Context, shell string) (path string, fromShell bool) {
	if env, ok := loginEnv(ctx, shell); ok {
		return envValue(env, "PATH"), true
	}
	return os.Getenv("PATH"), false
}

// loginEnv asks shell for its interactive login environment, NUL separated
// after the marker. A read without PATH is a failed read.
func loginEnv(ctx context.Context, shell string) ([]string, bool) {
	tctx, cancel := context.WithTimeout(ctx, loginPathTimeout)
	defer cancel()
	cmd := exec.CommandContext(tctx, shell, "-lic", `printf '\036TOGO_ENV\036'; env -0`)
	// Stdin stays nil, which is /dev/null, so an rc file that reads input
	// sees EOF instead of blocking. A new session detaches the shell from
	// any controlling terminal the helper has, so interactive job control
	// cannot stop it, and makes it a process group that the timeout kills
	// whole: a child left holding stdout would otherwise keep Output waiting.
	cmd.Stdin = nil
	cmd.Env = probeEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	_, rest, ok := bytes.Cut(out, []byte(loginEnvMarker))
	if !ok {
		return nil, false
	}
	env := parseEnv(rest)
	if envValue(env, "PATH") == "" {
		return nil, false
	}
	return env, true
}

// probeEnv is the helper's environment with TERM set: launchd starts the
// helper without one, and rc files that test or use it misbehave.
func probeEnv() []string {
	env := os.Environ()
	if os.Getenv("TERM") == "" {
		env = append(env, "TERM=xterm-256color")
	}
	return env
}

// parseEnv splits `env -0` output into KEY=value entries, without the
// shell's own bookkeeping variables.
func parseEnv(b []byte) []string {
	var env []string
	for _, kv := range strings.Split(string(b), "\x00") {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" || strings.ContainsAny(k, "\n ") || skipLoginEnv[k] {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// envValue returns key's value in env, or "".
func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && k == key {
			return v
		}
	}
	return ""
}
