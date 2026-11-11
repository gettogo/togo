package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// stateMapper is a mapper that only reports state.
type stateMapper struct{ state map[string]string }

func (m *stateMapper) Name() string                                      { return "state" }
func (m *stateMapper) Map(harness.Record) ([]harness.Item, []harness.Op) { return nil, nil }
func (m *stateMapper) State() map[string]string                          { return m.state }

// turnEngine serves one session whose single feed is an event stream
// (hook false) or hooks (hook true), its mapper reporting working.
func turnEngine(id string, hook bool, working string, status string) *Engine {
	feed := spec.Feed{Mapper: "state"}
	if hook {
		feed.Hook = &spec.Hook{}
	} else {
		feed.SSE = &spec.HTTP{}
	}
	e := New(Config{Status: func(string) string { return status }})
	ad := &adapter{Adapter: &spec.Adapter{Feeds: map[string]spec.Feed{"events": feed}}, feedOrder: []string{"events"}}
	e.sessions[id] = &sess{e: e, id: id, ad: ad,
		mappers: map[string]harness.Mapper{"state": &stateMapper{state: map[string]string{"working": working}}}}
	return e
}

// The pane watcher asks before it calls a silent pane done: a turn the
// agent's own event stream reports running is vouched for, whatever the
// pane says; a hook-fed turn, an ended one, or a gone agent is not.
func TestTurnRunningVouchesOnlyForStreamFedTurns(t *testing.T) {
	const id = "s_turnrun001"
	for _, c := range []struct {
		name            string
		hook            bool
		working, status string
		want            bool
	}{
		{"stream says working, pane idle", false, "true", "idle", true},
		{"stream says idle", false, "false", "working", false},
		{"hooks say working", true, "true", "idle", false},
		{"agent exited", false, "true", "ended", false},
	} {
		if got := turnEngine(id, c.hook, c.working, c.status).TurnRunning(id); got != c.want {
			t.Errorf("%s: TurnRunning = %v, want %v", c.name, got, c.want)
		}
	}
	if turnEngine(id, false, "true", "idle").TurnRunning("s_unknown001") {
		t.Error("an unknown session is vouched for")
	}
}
