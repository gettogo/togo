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

// fakereload lists its commands through a CLI that counts its runs, cached
// for five minutes as OpenCode's are, and renewed by the mapper's
// commands_rev.
func init() {
	harness.RegisterAdapterFile("fakereload", []byte(`
adapter: fakereload
name: Fake reload
revision: 1
match: { agents: [fakereload] }
capabilities: [chat, commands]
feeds:
  events: { hook: { events: [Event] }, mapper: fake.events, stability: documented }
sources:
  commands: { cli: { argv: [cmds] }, cache: 5m, cache_rev: commands_rev, stability: documented }
commands:
  source: commands
`))
}

// Seen live on OpenCode 2.3.0: the harness reloaded its config and listed a
// new command, and the / menu served its five-minute copy. A source can
// name a state value its mapper changes when the harness says the data
// changed; a copy cached under another value is read again, once.
func TestCachedSourceIsReadAgainWhenItsRevChanges(t *testing.T) {
	const id = "s_fakereload1"
	bin, home := t.TempDir(), t.TempDir()
	runs := filepath.Join(home, "runs.log")
	script := "#!/bin/sh\necho run >> " + runs + "\ncat " + filepath.Join(home, "cmds.json") + "\n"
	if err := os.WriteFile(filepath.Join(bin, "cmds"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(list string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "cmds.json"), []byte(list), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`[{"name":"init"}]`)
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakereload", Managed: true, Cwd: home, Command: []string{"fakereload"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.05,
	})
	t.Cleanup(func() { e.Forget(id) })
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	menu := func() string {
		t.Helper()
		res, err := e.Complete(ctx, harness.CompleteRequest{Session: id, Kind: harness.CompleteCommand})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range res.Items {
			out = append(out, c.Title)
		}
		return strings.Join(out, " ")
	}
	count := func() int {
		b, _ := os.ReadFile(runs)
		return strings.Count(string(b), "run")
	}
	rev := func(v string) {
		t.Helper()
		deliver(id, map[string]any{"state": map[string]string{"commands_rev": v}})
		s, err := e.session(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.stateLocked()["commands_rev"] == v
		})
	}

	if got := menu(); got != "/init" {
		t.Fatalf("menu = %q", got)
	}
	write(`[{"name":"init"},{"name":"audit-echo"}]`)
	if got := menu(); got != "/init" || count() != 1 {
		t.Fatalf("menu = %q after %d reads; want the cached copy", got, count())
	}
	rev("evt_1")
	if got := menu(); got != "/init /audit-echo" {
		t.Fatalf("menu = %q after the harness said its commands changed", got)
	}
	// The same rev again serves the cache: one read per change.
	menu()
	if n := count(); n != 2 {
		t.Fatalf("commands read %d times, want 2", n)
	}
	// A read that fails after a change still answers with the last copy.
	if err := os.Remove(filepath.Join(home, "cmds.json")); err != nil {
		t.Fatal(err)
	}
	rev("evt_2")
	if got := menu(); got != "/init /audit-echo" {
		t.Fatalf("menu = %q when the read failed", got)
	}
}
