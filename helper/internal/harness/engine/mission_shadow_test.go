package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// fakechildren fills Agents from a mapper and from a source that prints
// children.json from the session folder, as OpenCode's adapters do.
func init() {
	harness.RegisterAdapterFile("fakechildren", []byte(`
adapter: fakechildren
name: Fake children
revision: 1
match: { agents: [fakechildren] }
capabilities: [chat, agents]
feeds:
  events: { hook: { events: [Event] }, mapper: fake.events, stability: documented }
sources:
  children: { cli: { argv: [children] }, stability: documented }
mission:
  agents: { source: children, refresh: 1h }
`))
}

// Seen live on OpenCode 2.3.0: one child session was two rows under Agents,
// "ses_…" from the mapper (with its chat and Stop) and "src-ses_…" from the
// children source. The rows below are the two from that capture. A row a
// mapper keeps stands for the source's row of the same id, whichever of
// the two came first.
func TestOneAgentRowWhenMapperAndSourceNameTheSameChild(t *testing.T) {
	const id = "s_fakechild01"
	const child = "ses_e28d51c92ffeuZKPOJEAMrXUPw"
	bin, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "children"), []byte("#!/bin/sh\ncat children.json\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	list := func(json string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "children.json"), []byte(json), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	list(`[]`)
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakechildren", Managed: true, Cwd: home, Command: []string{"fakechildren"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
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
	agents := func() []harness.Row {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, sec := range s.mission.snapshot().Sections {
			if sec.Kind == harness.SectionAgents {
				return sec.Rows
			}
		}
		return nil
	}
	mapperRow := func(row string) {
		t.Helper()
		before := len(agents())
		deliver(id, map[string]any{"ops": []harness.Op{{Op: harness.OpUpsert, Section: harness.SectionAgents, Row: &harness.Row{
			ID: row, Title: "Audit read-only child", Subtitle: "general", State: harness.StateRunning,
			Open:    &harness.Open{Kind: harness.OpenChat, Ref: "agent-" + row},
			Actions: []harness.ActionRef{{ID: harness.ActionStop, Label: "Stop", Role: harness.RoleDestructive}}}}}})
		waitFor(t, func() bool {
			for _, r := range agents() {
				if r.ID == row {
					return true
				}
			}
			return len(agents()) > before
		})
	}
	only := func(when, want string) {
		t.Helper()
		rows := agents()
		if len(rows) != 1 || rows[0].ID != want {
			t.Fatalf("%s: rows %+v, want one row %s", when, rows, want)
		}
	}

	// The mapper's row first, then the source lists the same child.
	mapperRow(child)
	list(`[{"id":"` + child + `","title":"Audit read-only child","state":"running"}]`)
	e.refreshMission(ctx, s, harness.SectionAgents)
	only("mapper first", child)
	if r := agents()[0]; r.Open == nil || len(r.Actions) != 1 {
		t.Fatalf("the row that stayed lost its chat or Stop: %+v", r)
	}

	// The source's row first (a child the mapper learns of a moment later).
	const late = "ses_e28d51c92ffeLATERCHILD0001"
	list(`[{"id":"` + child + `","title":"Audit read-only child","state":"running"},{"id":"` + late + `","title":"Later","state":"running"}]`)
	e.refreshMission(ctx, s, harness.SectionAgents)
	if rows := agents(); len(rows) != 2 || rows[1].ID != "src-"+late {
		t.Fatalf("a child only the source knows: rows %+v", rows)
	}
	mapperRow(late)
	if rows := agents(); len(rows) != 2 || rows[0].ID != child || rows[1].ID != late {
		t.Fatalf("source first: rows %+v", rows)
	}
	// The next reads keep it that way.
	e.refreshMission(ctx, s, harness.SectionAgents)
	e.refreshMission(ctx, s, harness.SectionAgents)
	if rows := agents(); len(rows) != 2 || rows[0].ID != child || rows[1].ID != late {
		t.Fatalf("after two more reads: rows %+v", rows)
	}
}
