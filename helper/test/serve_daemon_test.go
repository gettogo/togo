package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/daemon/daemontest"
)

// TestServeLeavesNoDaemonBehind checks that the daemon `serve --stdio`
// detaches is gone once the test's cleanup has run.
func TestServeLeavesNoDaemonBehind(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	var home string
	pid := 0
	// Registered first, so it runs after every other cleanup.
	t.Cleanup(func() {
		if home != "" && daemontest.Running(home) {
			t.Errorf("a daemon still holds the lock for %s", home)
		}
		if pid != 0 && daemontest.Alive(pid) {
			t.Errorf("daemon %d survived the test", pid)
		}
	})
	bin := buildTogo(t)
	env, vars, _ := isolatedEnv(t)
	for _, v := range vars {
		if h, ok := strings.CutPrefix(v, "TOGO_HOME="); ok {
			home = h
		}
	}

	cmd := exec.Command(bin, "serve", "--stdio")
	cmd.Env = env
	c, stdin, exited, _ := startServe(t, cmd)
	helloE2E(c)
	var listed listResult
	c.call("session.list", map[string]any{}, &listed)
	stdin.Close()
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("serve still running 2s after stdin closed")
	}

	raw, err := os.ReadFile(daemon.PIDPath(home))
	if err != nil {
		log, _ := os.ReadFile(filepath.Join(home, "log", "daemon.log"))
		t.Fatalf("serve started no daemon: %v; daemon log %s", err, log)
	}
	if pid, err = strconv.Atoi(strings.TrimSpace(string(raw))); err != nil {
		t.Fatal(err)
	}
}
