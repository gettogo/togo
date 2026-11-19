package engine

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// OpenCode 2.x resumes by relaunching: the session's adapter starts again
// on the new run (Engine.restart) and keeps its item log, and the fresh
// mapper names the resumed conversation first (its --session flag). When
// the chat showed another conversation before the restart, that first
// name is a move: the chat starts over, and nothing of the conversation
// left behind stays. Named the same, nothing starts over.
func TestRestartedSessionNamingAnotherConversationMoves(t *testing.T) {
	const id = "s_convcarry1"
	e, _ := newFake(t, id, "1.2.3")
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	s := e.sessions[id]
	e.mu.Unlock()
	name := func(conv string) {
		s.mappers["fake.events"] = &replay{state: map[string]string{StateConversation: conv}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	e.convCarry.put(id, "B") // the chat showed B before the restart
	s.conv = ""
	name("A")
	if !s.movedLocked() {
		t.Fatal("a restarted session naming A after a chat on B did not move")
	}
	s.startOverLocked("fake.events")
	name("A")
	if s.movedLocked() || s.conv != "A" {
		t.Fatalf("after the start over: conv %q", s.conv)
	}

	e.convCarry.put(id, "A") // restarted onto the conversation it showed
	s.conv = ""
	if s.movedLocked() || s.conv != "A" {
		t.Fatalf("a restart onto the same conversation moved: conv %q", s.conv)
	}
	if e.convCarry.take(id) != "" {
		t.Fatal("the carried conversation was not taken")
	}
}

// carrier is a fake mapper that records the mapper handed to its Carry.
type carrier struct {
	replay
	got harness.Mapper
}

func (c *carrier) Carry(next harness.Mapper) { c.got = next }

// A start-over on another conversation hands the fresh mapper to the old
// one's Carry before the fresh one maps the announcing record: Codex keeps
// the top-level threads its feed described once per connection.
func TestStartOverHandsTheFreshMapperToTheCarrier(t *testing.T) {
	const id = "s_convcarry2"
	e, _ := newFake(t, id, "1.2.3")
	if _, err := e.Describe(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	s := e.sessions[id]
	e.mu.Unlock()
	old := &carrier{replay: replay{state: map[string]string{StateConversation: "B"}}}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mappers["fake.events"] = old
	fresh := s.startOverLocked("fake.events")
	if fresh == nil || old.got != fresh || s.mappers["fake.events"] != fresh {
		t.Fatalf("carry got %v, fresh mapper %v", old.got, fresh)
	}
}
