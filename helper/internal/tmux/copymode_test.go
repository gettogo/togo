package tmux_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// A phone scroll leaves the pane in copy mode, where tmux reads typed keys
// as navigation. Text the helper sends is for the program in the pane.
func TestSendKeysLeavesCopyMode(t *testing.T) {
	c := tmuxtest.New(t)
	out := rawSink(t, c, "s_abcdefghij")
	target := "=s_abcdefghij:"
	tm := func(args ...string) string {
		t.Helper()
		b, err := exec.Command(c.Bin, append(append([]string{}, c.Socket...), args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	// A second session scrolled back by its owner stays as it is.
	tm("new-session", "-d", "-s", "s_otherother", "cat")
	tm("copy-mode", "-t", "=s_otherother:")
	t.Cleanup(func() {
		if got := tm("display-message", "-p", "-t", "=s_otherother:", "#{pane_in_mode}"); got != "1" {
			t.Errorf("other session pane_in_mode = %q, want 1", got)
		}
	})
	tm("copy-mode", "-t", target)
	if got := tm("display-message", "-p", "-t", target, "#{pane_in_mode}"); got != "1" {
		t.Fatalf("pane_in_mode = %q before send, want 1", got)
	}
	if err := c.SendKeys(context.Background(), target, true, "/resume"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	waitFor(t, func() bool { return readFile(t, out) == "/resume" })
	if got := tm("display-message", "-p", "-t", target, "#{pane_in_mode}"); got != "0" {
		t.Fatalf("pane_in_mode = %q after send, want 0", got)
	}

	tm("copy-mode", "-t", target)
	if err := c.PasteText(context.Background(), target, "hi"); err != nil {
		t.Fatalf("PasteText: %v", err)
	}
	if err := c.SendKeys(context.Background(), target, false, "Enter"); err != nil {
		t.Fatalf("SendKeys Enter: %v", err)
	}
	waitFor(t, func() bool {
		return strings.HasSuffix(readFile(t, out), "hi\r") || strings.HasSuffix(readFile(t, out), "hi\x1b[201~\r")
	})
	if got := tm("display-message", "-p", "-t", target, "#{pane_in_mode}"); got != "0" {
		t.Fatalf("pane_in_mode = %q after paste, want 0", got)
	}
}
