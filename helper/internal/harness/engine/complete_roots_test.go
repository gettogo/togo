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

// A mention is written so that the harness reads back exactly that path.
// The cases are omp's own parser's: a bare mention ends at a space or an
// "@" and loses some punctuation at its ends; quotes take a whole path with
// no escapes. A path no form can hold is not offered. A harness that
// declares nothing gets bare paths as before.
func TestAMentionIsWrittenSoTheHarnessReadsThatPath(t *testing.T) {
	omp := &spec.Mentions{Quotes: []string{`"`, "'"}, BareStops: "@", BareLeading: "`\"'([{<", BareTrailing: ")]}>.,;:!?\"'`"}
	for path, want := range map[string]string{
		"docs/plain.txt":        "docs/plain.txt",
		`a"b'c.txt`:             `a"b'c.txt`, // both quotes, but bare reads back whole
		"my docs/a file.txt":    `"my docs/a file.txt"`,
		"/extra folder/far.txt": `"/extra folder/far.txt"`,
		"/proj/a@b.txt":         `"/proj/a@b.txt"`,
		"notes/done.":           `"notes/done."`,
		"(draft)/x.txt":         `"(draft)/x.txt"`,
		`say "hi".txt`:          `'say "hi".txt'`,
		`ends"`:                 `'ends"'`,
		"it's here.txt":         `"it's here.txt"`,
		"tab\there.txt":         "\"tab\there.txt\"",
	} {
		if got, ok := mentionOf(path, omp); !ok || got != want {
			t.Errorf("%q is written %q %v, want %q", path, got, ok, want)
		}
	}
	for _, path := range []string{`both " and ' here`, " leading space", "trailing space ", "two\nlines", ""} {
		if got, ok := mentionOf(path, omp); ok {
			t.Errorf("%q cannot be written but was: %q", path, got)
		}
	}
	items := []harness.Completion{{Insert: "@docs/plain.txt "}, {Insert: "@my docs/a.txt "}, {Insert: "@both \" and ' here "}}
	got := mentionForm(append([]harness.Completion(nil), items...), omp)
	if len(got) != 2 || got[0].Insert != "@docs/plain.txt " || got[1].Insert != "@\"my docs/a.txt\" " {
		t.Errorf("mentions = %+v", got)
	}
	if plain := mentionForm(append([]harness.Completion(nil), items...), nil); len(plain) != 3 || plain[1].Insert != "@my docs/a.txt " {
		t.Errorf("a harness with no rule had its mentions changed: %+v", plain)
	}
}

// Seen in review: a broad query filled the whole result from the session's
// own folder and an added folder's files never appeared. Each folder keeps
// a share, and room a small folder leaves goes to the others.
func TestALargeFolderDoesNotPushAnotherFoldersFilesOut(t *testing.T) {
	list := func(prefix string, n int) []harness.Completion {
		out := make([]harness.Completion, n)
		for i := range out {
			out[i].Title = prefix
		}
		return out
	}
	count := func(items []harness.Completion) map[string]int {
		m := map[string]int{}
		for _, it := range items {
			m[it.Title]++
		}
		return m
	}
	for name, c := range map[string]struct {
		sizes map[string]int
		want  map[string]int
	}{
		"all fit":         {map[string]int{"a": 3, "b": 4}, map[string]int{"a": 3, "b": 4}},
		"two large":       {map[string]int{"a": 80, "b": 80}, map[string]int{"a": 25, "b": 25}},
		"small and large": {map[string]int{"a": 80, "b": 5}, map[string]int{"a": 45, "b": 5}},
		"three":           {map[string]int{"a": 80, "b": 80, "c": 80}, map[string]int{"a": 17, "b": 17, "c": 16}},
		"one empty":       {map[string]int{"a": 80, "b": 0}, map[string]int{"a": 50}},
	} {
		var lists [][]harness.Completion
		for _, k := range []string{"a", "b", "c"} {
			if n, ok := c.sizes[k]; ok {
				lists = append(lists, list(k, n))
			}
		}
		got := shareLimit(lists, 50)
		if g := count(got); len(got) > 50 || len(g) != len(c.want) {
			t.Errorf("%s: %v, want %v", name, g, c.want)
		} else {
			for k, n := range c.want {
				if g[k] != n {
					t.Errorf("%s: %v, want %v", name, g, c.want)
				}
			}
		}
		for i := 1; i < len(got); i++ {
			if got[i].Title < got[i-1].Title {
				t.Errorf("%s: folders are not in order", name)
			}
		}
	}
}
