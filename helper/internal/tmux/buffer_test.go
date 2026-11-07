package tmux_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

// rawSink starts a session whose program turns on bracketed paste, puts
// the terminal in raw mode and copies every byte it reads to a file, which
// it returns once the program is ready.
func rawSink(t *testing.T, c *tmux.Client, name string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "in")
	ready := out + ".ready"
	script := `printf '\033[?2004h'; stty raw -echo; : > "$2"; exec cat > "$1"`
	if err := c.NewSession(context.Background(), tmux.NewSessionOpts{
		Name:    name,
		Command: []string{"/bin/sh", "-c", script, "sink", out, ready},
	}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	waitFor(t, func() bool { _, err := os.Stat(ready); return err == nil })
	return out
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func buffers(t *testing.T, c *tmux.Client) string {
	t.Helper()
	out, err := exec.Command(c.Bin, append(append([]string{}, c.Socket...), "list-buffers", "-F", "#{buffer_name}")...).Output()
	if err != nil {
		t.Fatalf("list-buffers: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestPasteTextIsBracketedAndLiteral(t *testing.T) {
	c := tmuxtest.New(t)
	out := rawSink(t, c, "s_abcdefghij")
	text := "first $(touch /tmp/x); `id`\n'q' \"z\" #{session_name} -- \\;\nlast;"
	if err := c.PasteText(context.Background(), "=s_abcdefghij:", text); err != nil {
		t.Fatalf("PasteText: %v", err)
	}
	// tmux sends newlines as carriage returns, as a terminal would.
	want := "\x1b[200~" + strings.ReplaceAll(text, "\n", "\r") + "\x1b[201~"
	waitFor(t, func() bool { return len(readFile(t, out)) >= len(want) })
	if got := readFile(t, out); got != want {
		t.Fatalf("pane read %q\nwant %q", got, want)
	}
	if b := buffers(t, c); b != "" {
		t.Errorf("buffers left behind: %q", b)
	}
}

func TestPasteTextConcurrentSendsStayWhole(t *testing.T) {
	c := tmuxtest.New(t)
	out := rawSink(t, c, "s_abcdefghij")
	texts := []string{strings.Repeat("a", 3000), strings.Repeat("b", 3000), strings.Repeat("c", 3000)}
	var wg sync.WaitGroup
	errs := make([]error, len(texts))
	for i, text := range texts {
		wg.Go(func() { errs[i] = c.PasteText(context.Background(), "=s_abcdefghij:", text) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("PasteText: %v", err)
		}
	}
	block := len("\x1b[200~") + 3000 + len("\x1b[201~")
	waitFor(t, func() bool { return len(readFile(t, out)) >= block*len(texts) })
	got := readFile(t, out)
	for _, text := range texts {
		if !strings.Contains(got, "\x1b[200~"+text+"\x1b[201~") {
			t.Errorf("paste of %c... arrived split or mixed", text[0])
		}
	}
	if b := buffers(t, c); b != "" {
		t.Errorf("buffers left behind: %q", b)
	}
}

func TestPasteTextMissingSession(t *testing.T) {
	c := tmuxtest.New(t)
	rawSink(t, c, "s_abcdefghij")
	err := c.PasteText(context.Background(), "=s_zzzzzzzzzz:", "hello")
	if !errors.Is(err, tmux.ErrNoSession) {
		t.Fatalf("PasteText to a missing session = %v, want ErrNoSession", err)
	}
	if b := buffers(t, c); b != "" {
		t.Errorf("buffers left behind: %q", b)
	}
}

func TestPaneCommand(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: "s_abcdefghij", Command: []string{"/bin/cat"}}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	waitFor(t, func() bool { cmd, _ := c.PaneCommand(ctx, "=s_abcdefghij:"); return cmd == "cat" })
	if _, err := c.PaneCommand(ctx, "=s_zzzzzzzzzz:"); !errors.Is(err, tmux.ErrNoSession) {
		t.Fatalf("PaneCommand of a missing session = %v, want ErrNoSession", err)
	}
}

func TestPaneGeometry(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: "s_abcdefghij", Command: []string{"/bin/cat"}}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	g, err := c.PaneGeometry(ctx, "=s_abcdefghij:")
	if err != nil || !strings.Contains(g, "x") || !strings.HasSuffix(g, "/0") {
		t.Fatalf("PaneGeometry = %q, %v, want WxH/0", g, err)
	}
	if _, err := c.PaneGeometry(ctx, "=s_zzzzzzzzzz:"); !errors.Is(err, tmux.ErrNoSession) {
		t.Fatalf("PaneGeometry of a missing session = %v, want ErrNoSession", err)
	}
}
