package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/session"
)

// fakeompswitch is omp's model switch over omp's own session mapper, with
// no session file yet: omp writes it only when the first turn starts.
func init() {
	harness.RegisterAdapterFile("fakeompswitch", []byte(`
adapter: fakeompswitch
name: omp
revision: 1
match: { agents: [fakeompswitch] }
capabilities: [chat, model, status]
input:
  line: '^╰─(?: (.*?))?(?:\s{2,}⇧⇥ to change thinking effort)?\s*$'
  clear: [C-u]
  dismiss: [Escape]
feeds:
  session: { jsonl: { glob: '/nonexistent-togo-omp-test/*.jsonl', follow: true }, mapper: omp.session, stability: documented }
actions:
  switch_model:
    type: { text: '/switch {{.id}}', submit: true }
    verify: { source: session, select: .model, equals: '{{.id}}', within: 2s, required: true }
    stability: documented-command
`))
}

// switchPane plays omp's pane: a submitted /switch prints the
// "Session-only model" line when the model exists.
type switchPane struct {
	mu     sync.Mutex
	line   string
	status string
	known  map[string]bool
}

func (p *switchPane) Type(_ context.Context, _, text string, submit, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.line += text
	if submit {
		p.enterLocked()
	}
	return nil
}

func (p *switchPane) enterLocked() {
	if id, ok := strings.CutPrefix(p.line, "/switch "); ok && p.known[id] {
		p.status = "Session-only model: " + id + ". Use ⌥M or /model for roles."
	} else if ok {
		p.status = "Unknown model: " + id
	}
	p.line = ""
}

func (p *switchPane) Keys(_ context.Context, _ string, keys ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range keys {
		switch k {
		case "Enter":
			p.enterLocked()
		case "C-u":
			p.line = ""
		}
	}
	return nil
}

func (p *switchPane) Screen(context.Context, string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	in := "╰─"
	if p.line != "" {
		in += " " + p.line
	} else {
		in += "                                  ⇧⇥ to change thinking effort"
	}
	return "\n " + p.status + "\n\n π > ◑ Opus 5.5 > x\n" + in + "\n", nil
}

func newSwitchEngine(t *testing.T, id string, p *switchPane) *Engine {
	t.Helper()
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakeompswitch", Managed: true, Cwd: t.TempDir(), Command: []string{"fakeompswitch"}}},
		Typist:   p, Marker: &fakeMarker{}, Screen: p.Screen, Home: t.TempDir(),
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }},
		Scale:     0.05,
	})
	t.Cleanup(func() { e.Forget(id) })
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return e
}

func switchTo(e *Engine, id, model string) (harness.ActionResult, error) {
	raw, _ := json.Marshal(model)
	return e.Act(context.Background(), harness.ActionRequest{Session: id, Action: "switch_model", Target: model,
		Args: map[string]json.RawMessage{"id": raw}})
}

// Before omp's first turn there is no session record: the switch is
// confirmed from the pane, and the model chip shows what omp now uses.
func TestOmpSwitchConfirmedFromPaneBeforeFirstTurn(t *testing.T) {
	const id = "s_ompswitch01"
	p := &switchPane{known: map[string]bool{"anthropic/claude-haiku-4-5": true}}
	e := newSwitchEngine(t, id, p)
	res, err := switchTo(e, id, "anthropic/claude-haiku-4-5")
	if err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("switch = %+v, %v; want applied", res, err)
	}
	if st := statusOf(e, id); st == nil || st.Model != "anthropic/claude-haiku-4-5" {
		t.Fatalf("status %+v; want the switched model", st)
	}
}

// A switch omp does not show fails and says so: never "sent", never
// "waiting for omp".
func TestOmpSwitchNotTakenFails(t *testing.T) {
	const id = "s_ompswitch02"
	p := &switchPane{known: map[string]bool{"anthropic/claude-haiku-4-5": true}}
	e := newSwitchEngine(t, id, p)
	if res, _ := switchTo(e, id, "anthropic/claude-haiku-4-5"); res.Result != harness.ResultApplied {
		t.Fatalf("first switch %+v", res)
	}
	res, err := switchTo(e, id, "anthropic/claude-opus-5-5")
	if err != nil || res.Result != harness.ResultFailed {
		t.Fatalf("switch = %+v, %v; want failed", res, err)
	}
	if !strings.Contains(res.Message, "did not take the change") || !strings.Contains(res.Message, "claude-haiku-4-5") {
		t.Fatalf("message %q; want it to say what omp still uses", res.Message)
	}
	if st := statusOf(e, id); st == nil || st.Model != "anthropic/claude-haiku-4-5" {
		t.Fatalf("status %+v; want the model omp still uses", st)
	}
}
