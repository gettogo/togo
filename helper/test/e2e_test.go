// Package test holds end-to-end checks that drive the built togo binary the
// way the iOS client does: framed JSON-RPC over the stdin and stdout pipes.
package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
)

var labelSeq atomic.Int32

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type reply struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type pipeClient struct {
	t      *testing.T
	stdin  io.WriteCloser
	stdout io.Reader
	nextID int
}

func (c *pipeClient) call(method string, params any, result any) {
	c.t.Helper()
	c.nextID++
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params})
	if err != nil {
		c.t.Fatal(err)
	}
	if err := rpc.WriteFrame(c.stdin, req); err != nil {
		c.t.Fatalf("%s: write: %v", method, err)
	}
	payload, err := rpc.ReadFrame(c.stdout)
	if err != nil {
		c.t.Fatalf("%s: read: %v", method, err)
	}
	var r reply
	if err := json.Unmarshal(payload, &r); err != nil {
		c.t.Fatalf("%s: reply %s: %v", method, payload, err)
	}
	if r.Error != nil {
		c.t.Fatalf("%s: error %d %s", method, r.Error.Code, r.Error.Message)
	}
	if r.ID != c.nextID {
		c.t.Fatalf("%s: reply id %d, want %d", method, r.ID, c.nextID)
	}
	if err := json.Unmarshal(r.Result, result); err != nil {
		c.t.Fatalf("%s: result %s: %v", method, r.Result, err)
	}
}

type sessionInfo struct {
	ID      string `json:"id"`
	Bib     string `json:"bib"`
	Agent   string `json:"agent"`
	Managed bool   `json:"managed"`
}

type listResult struct {
	Sessions []sessionInfo `json:"sessions"`
}

func buildTogo(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "togo")
	out, err := exec.Command("go", "build", "-o", bin, "../cmd/togo").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// isolatedEnv keeps the helper off the owner's tmux sockets: the test label
// replaces "togo", TMUX_TMPDIR points the default socket at an empty
// directory, and an empty TMUX stops tmux from assuming it is nested. vars
// holds only those settings, for tests that build an environment from
// scratch; env is the test's own environment plus vars.
func isolatedEnv(t *testing.T) (env, vars []string, label string) {
	t.Helper()
	// Unix socket paths are capped near 104 bytes, which t.TempDir() can
	// exceed on macOS.
	tmuxDir, err := os.MkdirTemp("/tmp", "togo-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	label = fmt.Sprintf("togo-test-%d-%d", os.Getpid(), labelSeq.Add(1))
	vars = []string{
		"TOGO_HOME=" + t.TempDir(),
		"TOGO_TMUX_LABEL=" + label,
		"TMUX=",
		"TMUX_TMPDIR=" + tmuxDir,
	}
	env = append(os.Environ(), vars...)
	// kill-server must see the same TMUX_TMPDIR as the helper, or it looks in
	// the default socket directory and leaks the private server.
	t.Cleanup(func() {
		kill := exec.Command("tmux", "-L", label, "kill-server")
		kill.Env = env
		kill.Run()
		os.RemoveAll(tmuxDir)
	})
	return env, vars, label
}

// startServe starts cmd with framed pipes and returns a client for it, its
// stdin, a channel that receives its exit, and its stderr.
func startServe(t *testing.T, cmd *exec.Cmd) (*pipeClient, io.WriteCloser, <-chan error, *bytes.Buffer) {
	t.Helper()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	// A hung helper blocks ReadFrame; killing it turns that into an EOF so
	// the test fails in seconds instead of at the go test timeout.
	watchdog := time.AfterFunc(8*time.Second, func() { cmd.Process.Kill() })
	t.Cleanup(func() {
		watchdog.Stop()
		cmd.Process.Kill()
	})
	return &pipeClient{t: t, stdin: stdin, stdout: stdout}, stdin, exited, &stderr
}

func helloE2E(c *pipeClient) {
	c.t.Helper()
	var hello struct {
		Protocol int `json:"protocol"`
		Helper   struct {
			Version string `json:"version"`
		} `json:"helper"`
	}
	c.call("hello", map[string]any{
		"protocol":     1,
		"client":       map[string]string{"name": "togo-e2e", "version": "0"},
		"capabilities": []string{},
	}, &hello)
	if hello.Protocol != 1 || hello.Helper.Version == "" {
		c.t.Fatalf("hello result = %+v, want protocol 1 and a helper version", hello)
	}
}

func TestServeOverPipe(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	bin := buildTogo(t)
	env, _, label := isolatedEnv(t)

	cmd := exec.Command(bin, "serve", "--stdio")
	cmd.Env = env
	c, stdin, exited, stderr := startServe(t, cmd)

	helloE2E(c)

	var created sessionInfo
	c.call("session.create", map[string]any{"agent": "shell", "cwd": t.TempDir()}, &created)
	if !regexp.MustCompile(`^s_[a-z2-7]{10}$`).MatchString(created.ID) || !created.Managed {
		t.Fatalf("created = %+v, want a managed s_ id", created)
	}

	var listed listResult
	c.call("session.list", map[string]any{}, &listed)
	if len(listed.Sessions) != 1 || listed.Sessions[0].ID != created.ID || listed.Sessions[0].Bib != "01" {
		t.Fatalf("list = %+v, want only %s with bib 01", listed.Sessions, created.ID)
	}

	var attach struct {
		Argv []string `json:"argv"`
	}
	c.call("session.attachCommand", map[string]any{"id": created.ID}, &attach)
	if n := len(attach.Argv); n == 0 || attach.Argv[n-1] != "="+created.ID {
		t.Fatalf("attach argv = %q, want last element =%s", attach.Argv, created.ID)
	}
	if !strings.Contains(strings.Join(attach.Argv, " "), label) {
		t.Errorf("attach argv = %q, want socket label %s", attach.Argv, label)
	}

	var killed json.RawMessage
	c.call("session.kill", map[string]any{"id": created.ID}, &killed)

	c.call("session.list", map[string]any{}, &listed)
	if len(listed.Sessions) != 0 {
		t.Fatalf("list after kill = %+v, want empty", listed.Sessions)
	}

	stdin.Close()
	select {
	case err := <-exited:
		if err != nil {
			t.Errorf("serve exited with %v, stderr %q", err, stderr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve still running 2s after stdin closed")
	}
}

// TestServeUnderBareSSHEnv runs the helper the way sshd runs an exec
// request: a minimal PATH without the directories package managers install
// tmux into, and no SHELL, so the helper must find tmux on its own and hand
// back an attach argv the app can run under the same minimal PATH.
func TestServeUnderBareSSHEnv(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	bin := buildTogo(t)
	_, vars, label := isolatedEnv(t)

	args := append([]string{"-i", "HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}, vars...)
	cmd := exec.Command("/usr/bin/env", append(args, bin, "serve", "--stdio")...)
	c, stdin, exited, stderr := startServe(t, cmd)

	helloE2E(c)

	var created sessionInfo
	c.call("session.create", map[string]any{"agent": "shell", "cwd": t.TempDir()}, &created)
	if !created.Managed || created.Bib != "01" {
		t.Fatalf("created = %+v, want managed session 01", created)
	}

	var listed listResult
	c.call("session.list", map[string]any{}, &listed)
	if len(listed.Sessions) != 1 || listed.Sessions[0].ID != created.ID {
		t.Fatalf("list = %+v, want only %s", listed.Sessions, created.ID)
	}

	var attach struct {
		Argv []string `json:"argv"`
	}
	c.call("session.attachCommand", map[string]any{"id": created.ID}, &attach)
	if len(attach.Argv) == 0 || !filepath.IsAbs(attach.Argv[0]) {
		t.Fatalf("attach argv = %q, want an absolute tmux path first", attach.Argv)
	}
	if !strings.Contains(strings.Join(attach.Argv, " "), label) {
		t.Errorf("attach argv = %q, want socket label %s", attach.Argv, label)
	}

	stdin.Close()
	select {
	case err := <-exited:
		if err != nil {
			t.Errorf("serve exited with %v, stderr %q", err, stderr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve still running 2s after stdin closed")
	}
}
