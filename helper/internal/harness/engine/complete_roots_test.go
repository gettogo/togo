package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Files in a session's additional workspace folders are offered after the
// session's own, each by its absolute path. A folder inside the session's
// own is not listed twice, and without any additional folder nothing
// changes.
func TestFilesInAdditionalWorkspaceFoldersCompleteByAbsolutePath(t *testing.T) {
	const id = "s_fakefake41"
	e, _ := newFake(t, id, "1.2.3")
	ctx := context.Background()
	s, err := e.served(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cwd := s.filesDir(ctx)
	extra := filepath.Join(t.TempDir(), "with space")
	inside := filepath.Join(cwd, "pkg")
	for path, body := range map[string]string{
		filepath.Join(cwd, "neutral-here.txt"):         "x",
		filepath.Join(inside, "neutral-inside.txt"):    "x",
		filepath.Join(extra, "sub", "neutral-far.txt"): "x",
		filepath.Join(extra, "neutral-here.txt"):       "x",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	complete := func() []harness.Completion {
		res, err := e.Complete(ctx, harness.CompleteRequest{Session: id, Kind: harness.CompleteFile, Query: "neutral"})
		if err != nil {
			t.Fatal(err)
		}
		return res.Items
	}
	before := complete()
	if len(before) != 2 {
		t.Fatalf("without additional folders: %+v", before)
	}
	incomplete := false
	e.cfg.ExtraRoots = func(_ context.Context, session string) ([]string, bool) {
		if session != id {
			return nil, false
		}
		return []string{extra, inside}, incomplete
	}
	got := complete()
	if len(got) != 4 {
		t.Fatalf("with additional folders: %+v", got)
	}
	for i, it := range got[:2] {
		if it.Insert != before[i].Insert || it.Detail != before[i].Detail {
			t.Errorf("the session's own files changed: %+v", it)
		}
	}
	real, _ := filepath.EvalSymlinks(extra)
	for _, it := range got[2:] {
		path := strings.TrimSuffix(strings.TrimPrefix(it.Insert, "@"), " ")
		if !filepath.IsAbs(path) || !(strings.HasPrefix(path, extra) || strings.HasPrefix(path, real)) || it.Detail != filepath.Dir(path) {
			t.Errorf("an additional folder's file is not named by its absolute path: %+v", it)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the inserted path does not exist: %q", path)
		}
	}
	// When the folders are not all known, files are not offered as if they
	// were: the completion is refused with a fixed text that names no path.
	// Commands are unaffected.
	incomplete = true
	_, err = e.Complete(ctx, harness.CompleteRequest{Session: id, Kind: harness.CompleteFile, Query: "neutral"})
	var refused *harness.FallbackError
	if !errors.As(err, &refused) || refused.Reason != "roots" || refused.Detail != rootsIncompleteText || strings.Contains(refused.Detail, extra) {
		t.Errorf("incomplete folders: %v", err)
	}
	if _, err := e.Complete(ctx, harness.CompleteRequest{Session: id, Kind: harness.CompleteCommand}); err != nil {
		t.Errorf("commands were refused with the files: %v", err)
	}
}

// omp reads a bare mention only up to its first space, and a quoted one
// whole. Where a harness says so, a path with a space is written quoted, in
// the session's own folder and in an additional one alike; other paths stay
// bare, and a harness that says nothing gets bare paths as before.
func TestAMentionWithASpaceIsWrittenTheWayTheHarnessReadsIt(t *testing.T) {
	items := func() []harness.Completion {
		return []harness.Completion{
			{Insert: "@docs/plain.txt "}, {Insert: "@my docs/a file.txt "}, {Insert: "@/extra folder/far.txt "},
			{Insert: "@odd \"name\".txt "},
		}
	}
	got := mentionForm(items(), &spec.Mentions{Quote: "double"})
	want := []string{"@docs/plain.txt ", "@\"my docs/a file.txt\" ", "@\"/extra folder/far.txt\" "}
	if len(got) != len(want) {
		t.Fatalf("mentions = %+v", got)
	}
	for i := range want {
		if got[i].Insert != want[i] {
			t.Errorf("mention %d = %q, want %q", i, got[i].Insert, want[i])
		}
	}
	for _, m := range []*spec.Mentions{nil, {}} {
		if plain := mentionForm(items(), m); len(plain) != 4 || plain[1].Insert != "@my docs/a file.txt " {
			t.Errorf("a harness with no quoting rule had its mentions changed: %+v", plain)
		}
	}
}
