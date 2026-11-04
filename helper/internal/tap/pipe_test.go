package tap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

var stubBin string

const stubSource = `package main

import (
	"io"
	"os"
)

var out string

func main() {
	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		os.Exit(1)
	}
	defer f.Close()
	io.Copy(f, os.Stdin)
}
`

func TestMain(m *testing.M) {
	code := func() int {
		dir, err := os.MkdirTemp("", "tg")
		if err != nil {
			return 1
		}
		defer os.RemoveAll(dir)
		src := filepath.Join(dir, "stub.go")
		if err := os.WriteFile(src, []byte(stubSource), 0o600); err != nil {
			return 1
		}
		stubBin = filepath.Join(dir, "stub")
		outPath = filepath.Join(dir, "out")
		build := exec.Command("go", "build", "-ldflags", "-X main.out="+outPath, "-o", stubBin, src)
		build.Dir = dir
		if b, err := build.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "stub build: %v\n%s", err, b)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}

var outPath string

func TestPipeCommandRejectsBadID(t *testing.T) {
	for _, id := range []string{"s_abc;rm", "../x", "", "s_abcdefghij; id"} {
		if _, err := PipeCommand("/usr/bin/togo", id); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
	if _, err := PipeCommand("togo", "s_abcdefghij"); err == nil {
		t.Error("relative path accepted")
	}
	got, err := PipeCommand("/usr/bin/togo", "s_abcdefghij")
	if err != nil || got != "exec '/usr/bin/togo' tap s_abcdefghij" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestPipeCommandQuotesPath(t *testing.T) {
	got, err := PipeCommand("/opt/my dir/it's/togo", "s_abcdefghij")
	if err != nil {
		t.Fatal(err)
	}
	want := `exec '/opt/my dir/it'\''s/togo' tap s_abcdefghij`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// The shell must hand the path back intact.
	out, err := exec.Command("sh", "-c", "printf %s "+strings.TrimSuffix(strings.TrimPrefix(got, "exec "), " tap s_abcdefghij")).Output()
	if err != nil || string(out) != "/opt/my dir/it's/togo" {
		t.Fatalf("shell saw %q, %v", out, err)
	}
}

func TestAttachDeliversBell(t *testing.T) {
	c := tmuxtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const id = "s_abcdefghij"
	_ = os.Remove(outPath)
	if err := c.NewSession(ctx, tmux.NewSessionOpts{
		Name:    id,
		Command: []string{"sh", "-c", "sleep 1; printf 'x\\007y'; sleep 30"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := Attach(ctx, c, id, stubBin); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, _ := os.ReadFile(outPath)
		if strings.Contains(string(b), "x\ay") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bell never reached the tap, got %q", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := Detach(ctx, c, id); err != nil {
		t.Fatal(err)
	}
}
