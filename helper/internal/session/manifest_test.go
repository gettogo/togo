package session_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
)

func TestManifestTracksCreateRenameKill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newService(t)
	dir := realpath(t, t.TempDir())
	recorder(t, s, dir, "claude")
	a := mustCreate(t, s, session.CreateOpts{Agent: "claude", Args: []string{"--model", "opus"}, Name: "one", Cwd: dir})
	b := mustCreate(t, s, session.CreateOpts{Cwd: dir})
	all, err := s.Manifest.All()
	if err != nil {
		t.Fatal(err)
	}
	want := []session.ManifestEntry{
		{ID: a.ID, Bib: "01", Name: "one", Agent: "claude", Cwd: dir, Command: []string{"claude", "--model", "opus"}, CreatedAt: fixedNow},
		{ID: b.ID, Bib: "02", Name: "Shell", Agent: "shell", Cwd: dir, CreatedAt: fixedNow},
	}
	if !slices.EqualFunc(all, want, entryEqual) {
		t.Fatalf("after create\n%+v\nwant\n%+v", all, want)
	}
	if _, err := s.Rename(t.Context(), a.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Kill(t.Context(), b.ID); err != nil {
		t.Fatal(err)
	}
	all, _ = s.Manifest.All()
	want = want[:1]
	want[0].Name = "renamed"
	if !slices.EqualFunc(all, want, entryEqual) {
		t.Fatalf("after rename and kill\n%+v\nwant\n%+v", all, want)
	}
}

func entryEqual(a, b session.ManifestEntry) bool {
	return a.ID == b.ID && a.Bib == b.Bib && a.Name == b.Name && a.Agent == b.Agent && a.Cwd == b.Cwd &&
		slices.Equal(a.Command, b.Command) && a.CreatedAt.Equal(b.CreatedAt) && a.EndedAt.Equal(b.EndedAt)
}

func TestManifestFile0600(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "home")
	m := &session.Manifest{Path: filepath.Join(dir, "sessions.json")}
	e := session.ManifestEntry{ID: "s_abcdefghij", Bib: "01", Name: "Shell", Agent: "shell", Cwd: "/tmp", CreatedAt: fixedNow}
	for range 3 {
		if err := m.Put(e); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(m.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
		t.Errorf("folder mode %v, want 0700", d.Mode().Perm())
	}
	names, _ := os.ReadDir(dir)
	for _, n := range names {
		if n.Name() != "sessions.json" && n.Name() != "sessions.json.lock" {
			t.Errorf("left behind %s", n.Name())
		}
	}
	b, _ := os.ReadFile(m.Path)
	if !strings.HasPrefix(string(b), `{"v":1,`) {
		t.Errorf("file %s, want version 1", b)
	}
}

// TestManifestSkipsTamperedEntries edits the file by hand: entries Create
// could not have written are skipped and logged without their content.
func TestManifestSkipsTamperedEntries(t *testing.T) {
	dir := t.TempDir()
	var logs []string
	m := &session.Manifest{Path: filepath.Join(dir, "sessions.json"), Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }}
	good := `{"id":"s_abcdefghij","bib":"01","name":"Shell","agent":"shell","cwd":"/tmp","command":null,"createdAt":"2026-11-03T12:00:00Z"}`
	bad := []string{
		`{"id":"../SECRET","bib":"02","name":"Shell","agent":"shell","cwd":"/tmp","command":null,"createdAt":"2026-11-03T12:00:00Z"}`,
		`{"id":"s_bbbbbbbbbb","bib":"02","name":"Shell","agent":"shell","cwd":"/tmp/../SECRET","command":null,"createdAt":"2026-11-03T12:00:00Z"}`,
		`{"id":"s_cccccccccc","bib":"03","name":"Shell","agent":"shell","cwd":"SECRET","command":null,"createdAt":"2026-11-03T12:00:00Z"}`,
		`{"id":"s_dddddddddd","bib":"04","name":"x","agent":"claude","cwd":"/tmp","command":["sh","-c","SECRET"],"createdAt":"2026-11-03T12:00:00Z"}`,
		`{"id":"s_eeeeeeeeee","bib":"05","name":"x","agent":"custom","cwd":"/tmp","command":["../SECRET"],"createdAt":"2026-11-03T12:00:00Z"}`,
		`{"id":"s_ffffffffff","bib":"06","name":"x","agent":"claude","cwd":"/tmp","command":["claude","SECRET\u0007"],"createdAt":"2026-11-03T12:00:00Z"}`,
		`{"id":"s_gggggggggg","bib":"07","name":"x","agent":"SECRET","cwd":"/tmp","command":null,"createdAt":"2026-11-03T12:00:00Z"}`,
		`{"id":"s_hhhhhhhhhh","bib":"00","name":"x","agent":"shell","cwd":"/tmp","command":null,"createdAt":"2026-11-03T12:00:00Z"}`,
		`{"id":"s_iiiiiiiiii","bib":"08","name":"x","agent":"shell","cwd":"/tmp","command":["SECRET"],"createdAt":"2026-11-03T12:00:00Z"}`,
		`{"id":"s_jjjjjjjjjj","bib":"09","name":"x","agent":"claude","cwd":"/tmp","command":["claude","` + strings.Repeat("A", 5000) + `"],"createdAt":"2026-11-03T12:00:00Z"}`,
	}
	file := `{"v":1,"sessions":[` + good + "," + strings.Join(bad, ",") + `]}`
	if err := os.WriteFile(m.Path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	all, err := m.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != "s_abcdefghij" || !all[0].CreatedAt.Equal(fixedNow) {
		t.Fatalf("entries %+v, want only the good one", all)
	}
	if len(logs) != len(bad) {
		t.Errorf("%d log lines for %d bad entries: %q", len(logs), len(bad), logs)
	}
	for _, l := range logs {
		if strings.Contains(l, "SECRET") || strings.Contains(l, "AAAA") {
			t.Errorf("log quotes the entry: %q", l)
		}
	}
	if err := m.Put(session.ManifestEntry{ID: "s_bbbbbbbbbb", Bib: "02", Name: "x", Agent: "shell", Cwd: "relative", CreatedAt: time.Now()}); err == nil {
		t.Error("Put accepted a relative folder")
	}
}

func TestManifestIgnoresOtherVersionsAndJunk(t *testing.T) {
	for _, body := range []string{`{"v":2,"sessions":[]}`, `not json`, ``} {
		m := &session.Manifest{Path: filepath.Join(t.TempDir(), "sessions.json")}
		if err := os.WriteFile(m.Path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if all, err := m.All(); err != nil || len(all) != 0 {
			t.Errorf("%q: %+v %v, want nothing", body, all, err)
		}
	}
}
