package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A lines tail emits whole lines, blank ones kept, and holds a partial
// last line until its newline arrives.
func TestLinesTailEmitsWholeLines(t *testing.T) {
	env, h := testEnv(t)
	p := filepath.Join(h, "chat.md")
	_ = os.WriteFile(p, []byte("# a\r\n\n#### hi  \npart"), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &collector{}
	go func() {
		_ = File{kind: "jsonl"}.Watch(ctx, sspec("t", map[string]any{"path": p, "follow": true, "format": "lines"}), env, c.emit)
	}()
	got := c.wait(t, 1)
	if got[0] != `["# a","","#### hi  "]` {
		t.Fatalf("first = %v", got)
	}
	fh, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = fh.WriteString("ial\n")
	fh.Close()
	got = c.wait(t, 2)
	if got[1] != `["partial"]` {
		t.Fatalf("second = %v", got)
	}
}
