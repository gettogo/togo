package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// fakemodes lists its modes through a CLI that prints agents.json from the
// session folder, and fails when the file is gone.
func init() {
	harness.RegisterAdapterFile("fakemodes", []byte(`
adapter: fakemodes
name: Fake modes
revision: 1
match: { agents: [fakemodes] }
capabilities: [mode]
sources:
  agents: { cli: { argv: [agents] }, cache: 1m, stability: documented }
actions:
  set_mode: { type: { text: '/agent {{.id}}', submit: true }, stability: documented-command }
modes:
  choices: [{ id: build, label: Build }, { id: plan, label: Plan }]
  source: agents
  set: set_mode
`))
}

// Seen live on OpenCode 2.3.0: a custom primary agent was listed by the
// server and could be set, but the mode chip offered only the two modes
// written in the adapter. Modes come from the source when it answers, and
// from the written choices when it does not.
func TestModeChoicesComeFromTheSource(t *testing.T) {
	const id = "s_fakemodes01"
	bin, home := t.TempDir(), t.TempDir()
	script := "#!/bin/sh\ncat agents.json\n"
	if err := os.WriteFile(filepath.Join(bin, "agents"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakemodes", Managed: true, Cwd: home, Command: []string{"fakemodes"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home, Now: func() time.Time { return now },
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.05,
	})
	t.Cleanup(func() { e.Forget(id) })
	modes := func() string {
		t.Helper()
		d, err := e.Describe(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range d.Modes {
			out = append(out, m.ID+"="+m.Label+"("+m.Detail+")")
		}
		return strings.Join(out, " ")
	}
	const written = "build=Build() plan=Plan()"

	// The source cannot be read: the written choices.
	if got := modes(); got != written {
		t.Fatalf("with no source answer: %s", got)
	}

	// It answers: its list, in its order. An entry with no id is left out,
	// a repeated id is listed once, a missing name falls back to the id.
	list := `[{"id":"build","name":"Build"},{"id":"plan","name":"Plan","description":"Read only"},
		{"name":"no id"},{"id":"audit-observer","name":"audit-observer"},{"id":"plan","name":"Again"},{"id":"bare"}]`
	if err := os.WriteFile(filepath.Join(home, "agents.json"), []byte(list), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := modes(); got != "build=Build() plan=Plan(Read only) audit-observer=audit-observer() bare=bare()" {
		t.Fatalf("with a source answer: %s", got)
	}

	// An empty answer is not a list of modes: the written choices.
	now = now.Add(2 * time.Minute)
	if err := os.WriteFile(filepath.Join(home, "agents.json"), []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := modes(); got != written {
		t.Fatalf("with an empty answer: %s", got)
	}

	// More modes than a menu holds: clipped, never sent whole.
	var many []string
	for i := 0; i < harness.MaxChoices+20; i++ {
		many = append(many, `{"id":"m`+strings.Repeat("x", i)+`","name":"`+strings.Repeat("n", 300)+`"}`)
	}
	now = now.Add(2 * time.Minute)
	if err := os.WriteFile(filepath.Join(home, "agents.json"), []byte("["+strings.Join(many, ",")+"]"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := e.Describe(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Modes) != harness.MaxChoices {
		t.Fatalf("%d modes, want %d", len(d.Modes), harness.MaxChoices)
	}
	if n := len([]rune(d.Modes[0].Label)); n > harness.MaxTitle {
		t.Fatalf("a label of %d runes left the helper", n)
	}
}
