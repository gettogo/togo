package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/browser"
)

// openURLWait bounds the daemon call; the daemon itself waits at most a few
// seconds for a phone to attach.
const openURLWait = 8 * time.Second

func init() {
	register("open-url", func(ctx context.Context, args []string, env map[string]string, _ io.Reader, _, stderr io.Writer) int {
		return runOpenURL(ctx, args, env, stderr)
	})
	addUsage("  open-url <url>                                open a web page on your phone")
}

// openFlags are the open(1) flags that change nothing about which page
// opens, so a call with them still goes to the phone. Any other flag (an
// app, a bundle id, stdin, a file) means the caller wants the Mac.
var openFlags = map[string]bool{
	"-g": true, "--background": true, "-n": true, "--new": true, "-F": true, "--fresh": true,
	"-j": true, "--hide": true, "-W": true, "--wait-apps": true,
}

// forwardable returns the one web URL a shim invocation opens, when the
// whole invocation is just that. as is the shim's name.
func forwardable(as string, args []string) (string, bool) {
	var operands []string
	if as == browser.OpenShim {
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "-u" && i+1 < len(args):
				operands = append(operands, args[i+1])
				i++
			case strings.HasPrefix(a, "-"):
				if !openFlags[a] {
					return "", false
				}
			default:
				operands = append(operands, a)
			}
		}
	} else {
		operands = args
	}
	if len(operands) != 1 {
		return "", false
	}
	u, err := browser.Validate(operands[0])
	return u, err == nil
}

// runOpenURL is what the open, xdg-open and BROWSER shims run. A single web
// URL goes to the phone through the daemon; when no phone takes it, or the
// arguments are anything else, the system command runs with the arguments
// untouched.
func runOpenURL(ctx context.Context, args []string, env map[string]string, stderr io.Writer) int {
	as := browser.BrowserShim
	if len(args) >= 2 && args[0] == "--as" {
		as, args = args[1], args[2:]
	}
	switch as {
	case browser.BrowserShim, browser.OpenShim, browser.XdgOpenShim:
	default:
		return usageError(stderr, errors.New("open-url: unknown --as"))
	}
	if u, ok := forwardable(as, args); ok && sendToPhone(ctx, env, u) {
		return exitOK
	}
	return passThrough(args, env, stderr)
}

func sendToPhone(ctx context.Context, env map[string]string, u string) bool {
	sock, err := tapSocketPath(env)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, openURLWait)
	defer cancel()
	var res browser.LocalResult
	p := browser.LocalParams{SessionID: claude.SessionID(env), URL: u}
	return localCall(ctx, sock, browser.MethodLocalOpen, p, &res) == nil && res.Delivered
}

// execSystem replaces this process; tests swap it.
var execSystem = func(path string, argv []string) error {
	return syscall.Exec(path, argv, os.Environ())
}

// passThrough runs the system opener with args as given, never through a
// shell: open on macOS, the first xdg-open on PATH outside the shim
// directory elsewhere.
func passThrough(args []string, env map[string]string, stderr io.Writer) int {
	path := browser.SystemOpen
	if runtime.GOOS != "darwin" {
		path = realXdgOpen(env)
		if path == "" {
			return fail(stderr, errors.New("no xdg-open found"))
		}
	}
	argv := append([]string{filepath.Base(path)}, args...)
	if err := execSystem(path, argv); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

func realXdgOpen(env map[string]string) string {
	shim := filepath.Clean(env[browser.EnvShimDir])
	for _, dir := range filepath.SplitList(env["PATH"]) {
		if dir == "" || filepath.Clean(dir) == shim {
			continue
		}
		p := filepath.Join(dir, browser.XdgOpenShim)
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	return ""
}
