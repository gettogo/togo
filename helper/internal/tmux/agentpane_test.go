package tmux_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// AgentPane names the pane respawn-pane restarts: with the window split,
// the active one, and after a respawn its new device, process and command.
func TestAgentPaneIsThePaneARespawnRestarts(t *testing.T) {
	c := tmuxtest.New(t)
	tm := func(args ...string) string {
		t.Helper()
		b, err := exec.Command(c.Bin, append(append([]string{}, c.Socket...), args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	tm("new-session", "-d", "-s", "s_agentpane1", "cat")
	tm("split-window", "-t", "=s_agentpane1:", "cat")
	tm("select-pane", "-t", "=s_agentpane1:.1")
	ctx := context.Background()
	before, err := c.AgentPane(ctx, "s_agentpane1")
	if err != nil || before.TTY != tm("display-message", "-p", "-t", "=s_agentpane1:.1", "#{pane_tty}") || before.PID == 0 {
		t.Fatalf("pane %+v %v, want the active pane", before, err)
	}
	if err := c.RespawnPane(ctx, "=s_agentpane1:", "", []string{"sh", "-c", "exec cat", "x", "a b"}); err != nil {
		t.Fatal(err)
	}
	after, err := c.AgentPane(ctx, "s_agentpane1")
	if err != nil || after.PID == before.PID || after.TTY != tm("display-message", "-p", "-t", "=s_agentpane1:.1", "#{pane_tty}") {
		t.Fatalf("after the respawn %+v %v (before %+v)", after, err, before)
	}
	if !strings.HasPrefix(after.StartCommand, "sh -c ") || !strings.Contains(after.StartCommand, "a b") {
		t.Fatalf("start command %q", after.StartCommand)
	}
	if p, err := c.AgentPane(ctx, "s_nosuchone1"); err == nil && p.TTY != "" {
		t.Fatalf("a missing session has pane %+v", p)
	}
}
