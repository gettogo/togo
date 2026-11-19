package engine

import (
	"context"
	"testing"
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
