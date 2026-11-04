package test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// TestE2EPromptOnScreenBeforeDaemonStartOpensApproval stops the daemon
// while an agent sits at a permission menu, starts a new one, and expects
// the menu to open an approval although the agent draws nothing more.
func TestE2EPromptOnScreenBeforeDaemonStartOpensApproval(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	bin := buildTogo(t)
	env, home, label := e2eEnv(t)
	sock := filepath.Join(home, "run", "togo.sock")
	first := startDaemon(t, bin, env)
	c := dialDaemon(t, sock)

	work := t.TempDir()
	var sess sessionInfo
	c.call("session.create", map[string]any{"agent": "shell", "cwd": work}, &sess)
	target := "=" + sess.ID + ":"
	waitFor(t, "the tap on the pane", 15*time.Second, func() bool {
		out, _ := tmuxCmd(env, label, "display-message", "-p", "-t", target, "#{pane_pipe}").Output()
		return strings.TrimSpace(string(out)) == "1"
	})

	// Stop the daemon first, so the menu below is drawn while none runs.
	c.conn.Close()
	if err := first.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatalf("first daemon: %v", err)
	}

	script := filepath.Join(work, "agent.sh")
	prompt, err := filepath.Abs("../internal/detect/testdata/claude/bash-permission.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(fakeAgent), 0o700); err != nil {
		t.Fatal(err)
	}
	keysOut := filepath.Join(work, "keys.out")
	if out, err := tmuxCmd(env, label, "send-keys", "-t", target, "sh "+script+" "+keysOut+" "+prompt, "Enter").CombinedOutput(); err != nil {
		t.Fatalf("send-keys: %v %s", err, out)
	}
	waitFor(t, "the menu on screen", 15*time.Second, func() bool {
		out, _ := tmuxCmd(env, label, "capture-pane", "-p", "-t", target).Output()
		return bytes.Contains(out, []byte("Do you want to proceed?"))
	})

	startDaemon(t, bin, env)
	c = dialDaemon(t, sock)
	var snap struct {
		Approvals []approvalInfo `json:"approvals"`
	}
	c.call("events.subscribe", map[string]any{"foreground": false}, &snap)
	for _, a := range snap.Approvals {
		if a.SessionID == sess.ID {
			return
		}
	}
	ev := c.waitEvent("approval.open for the menu already on screen", 20*time.Second, func(ev event) bool {
		return ev.Kind == wire.EventApprovalOpen && ev.SessionID != nil && *ev.SessionID == sess.ID
	})
	var a approvalInfo
	if err := json.Unmarshal(ev.Approval, &a); err != nil || a.Source != wire.SourceScreen {
		t.Fatalf("approval %s (%v), want a screen approval", ev.Approval, err)
	}
}
