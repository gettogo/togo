package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// Seen live on OpenCode 2.3.0: a conversation moved to a folder holding
// only ONLY_IN_ALTERNATE.txt, and the @ menu kept listing the folder the
// session started in. The menu lists the folder the mapper reports,
// confined as every path a harness names is: a folder outside the home and
// the session folder is not listed, nor is a path that is no folder.
func TestFileCompletionFollowsTheConversationFolder(t *testing.T) {
	const id = "s_fakemoved01"
	home, outside := t.TempDir(), t.TempDir()
	project, alternate := filepath.Join(home, "project"), filepath.Join(home, "alternate-project")
	for p, name := range map[string]string{project: "README.md", alternate: "ONLY_IN_ALTERNATE.txt", outside: "OUTSIDE.txt"} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(home, "link-out")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakereload", Managed: true, Cwd: project, Command: []string{"fakereload"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.05,
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	s, err := e.session(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	files := func() string {
		t.Helper()
		res, err := e.Complete(ctx, harness.CompleteRequest{Session: id, Kind: harness.CompleteFile})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range res.Items {
			out = append(out, strings.TrimSpace(c.Insert))
		}
		return strings.Join(out, " ")
	}
	moved := func(dir string) {
		t.Helper()
		deliver(id, map[string]any{"state": map[string]string{StateCwd: dir}})
		waitFor(t, func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.stateLocked()[StateCwd] == dir
		})
	}
	if got := files(); got != "@README.md" {
		t.Fatalf("before a move: %q", got)
	}
	moved(alternate)
	if got := files(); got != "@ONLY_IN_ALTERNATE.txt" {
		t.Fatalf("after the move: %q", got)
	}
	for _, bad := range []string{outside, link, "/", "../alternate-project", filepath.Join(alternate, "ONLY_IN_ALTERNATE.txt"), filepath.Join(home, "gone")} {
		moved(bad)
		if got := files(); got != "@README.md" {
			t.Fatalf("moved to %q: listed %q, want the session folder", bad, got)
		}
	}
}
