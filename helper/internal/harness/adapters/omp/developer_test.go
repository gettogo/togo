package omp

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Seen on omp 18.20, in thirteen places across real sessions: after a reply
// that stopped, omp adds a note of its own (role "developer", attribution
// "agent") and the model goes on, mostly with tool calls. Every such note
// was followed by more work. The turn had ended with the reply, so the
// session read as idle, and done, while the agent worked on.
const (
	devUser   = `{"type":"message","id":"d001","parentId":null,"timestamp":"2026-11-06T10:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"Check the parser"}]}}`
	devStop   = `{"type":"message","id":"d002","parentId":"d001","timestamp":"2026-11-06T10:00:05.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Checked."}],"stopReason":"stop"}}`
	devNote   = `{"type":"message","id":"d003","parentId":"d002","timestamp":"2026-11-06T10:00:05.100Z","message":{"role":"developer","content":[{"type":"text","text":"Two todo items are still open."}],"attribution":"agent","timestamp":1794000005100}}`
	devTool   = `{"type":"message","id":"d004","parentId":"d003","timestamp":"2026-11-06T10:00:08.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_d1","name":"read","arguments":{"path":"a.go"}}],"stopReason":"toolUse"}}`
	devResult = `{"type":"message","id":"d005","parentId":"d004","timestamp":"2026-11-06T10:00:08.500Z","message":{"role":"toolResult","toolCallId":"call_d1","toolName":"read","content":[{"type":"text","text":"package a"}],"isError":false}}`
	devDone   = `{"type":"message","id":"d006","parentId":"d005","timestamp":"2026-11-06T10:00:12.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Both closed."}],"stopReason":"stop"}}`
)

func TestWorkAfterOmpsOwnNoteIsATurn(t *testing.T) {
	m := newSession(env())
	run(m, []harness.Record{line("session", devUser), line("session", devStop)})
	if m.State()["working"] != "false" {
		t.Fatalf("working after the first reply = %q", m.State()["working"])
	}
	first := m.State()["turn"]
	run(m, []harness.Record{line("session", devNote)})
	if m.State()["working"] != "true" {
		t.Fatalf("working after omp's own note = %q, want true: the model goes on", m.State()["working"])
	}
	second := m.State()["turn"]
	if second == "" || second == first {
		t.Fatalf("turn after the note = %q, want a new one (was %q)", second, first)
	}
	out := run(m, []harness.Record{line("session", devTool), line("session", devResult)})
	if m.State()["working"] != "true" {
		t.Fatalf("working during the tool call = %q", m.State()["working"])
	}
	for _, it := range out.Items {
		if it.Kind == harness.KindTool && it.TurnID != second {
			t.Errorf("tool call in turn %q, want %q", it.TurnID, second)
		}
	}
	last := run(m, []harness.Record{line("session", devDone)}).last()
	if m.State()["working"] != "false" {
		t.Fatalf("working after the last reply = %q", m.State()["working"])
	}
	if tn := last[second]; tn.Turn == nil || tn.Turn.State != harness.StateOK || tn.Turn.Ended == nil {
		t.Errorf("the second turn at the end: %+v", tn.Turn)
	}
	// A note inside a running turn changes nothing; one in a subagent's
	// file is not the main conversation's.
	run(m, []harness.Record{line("session", `{"type":"message","id":"d007","parentId":"d006","timestamp":"2026-11-06T10:01:00.000Z","message":{"role":"user","content":[{"type":"text","text":"Again"}]}}`)})
	running := m.State()["turn"]
	run(m, []harness.Record{line("session", `{"type":"message","id":"d008","parentId":"d007","timestamp":"2026-11-06T10:01:01.000Z","message":{"role":"developer","content":[{"type":"text","text":"Reminder."}],"attribution":"agent"}}`)})
	if got := m.State()["turn"]; got != running {
		t.Errorf("a note inside a running turn started turn %q, want %q kept", got, running)
	}
}
