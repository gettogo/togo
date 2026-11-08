package local

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

func doneCount(p *fakePub) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, e := range p.events {
		if ne, ok := e.payload.(wire.NotifyEvent); ok && ne.Level == wire.LevelDone {
			n++
		}
	}
	return n
}

// Claude's Stop hook fires when the main agent hands off to a background
// subagent; work resumes 50 ms later. "Finished" waits a moment and is
// dropped when the agent works again first.
func TestDoneWaitsForWorkToResume(t *testing.T) {
	h, _, _, pub := setup()
	h.DoneGrace = 200 * time.Millisecond
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventStop, wire.HookData{Message: "handed off"})); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventTool, wire.HookData{Tool: "Bash"})); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if n := doneCount(pub); n != 0 {
		t.Fatalf("%d done pushes while the agent kept working", n)
	}
	// A Stop with nothing after it is reported after the grace.
	if _, err := call(t, h, wire.MethodLocalHook, hookEv(EventStop, wire.HookData{Message: "done"})); err != nil {
		t.Fatal(err)
	}
	if n := doneCount(pub); n != 0 {
		t.Fatal("done before the grace")
	}
	time.Sleep(400 * time.Millisecond)
	if n := doneCount(pub); n != 1 {
		t.Fatalf("done pushes = %d, want 1", n)
	}
}
