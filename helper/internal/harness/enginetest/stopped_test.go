package enginetest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// An engine the rig stopped is a helper that exited. A hub event it was
// still handling (an approval opened by its last timer, answered after
// the restart) must not serve the session again: the old engine would
// map every later hook a second time and append it to the session's hook
// store beside the new engine, and the next restart would replay each
// record twice (a tool that ended reads as interrupted).
func TestStoppedEngineServesTheSessionNoMore(t *testing.T) {
	r := New(t, Options{Agent: "droid", ID: NewID(), NoRedact: true})
	prompt := func(text string) {
		r.Deliver("UserPromptSubmit", []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"d1","prompt":"`+text+`"}`))
	}
	prompt("before the restart")
	if !r.WaitHook(0, 10*time.Second) {
		t.Fatal("the first prompt was not drawn")
	}
	old := r.Engine
	r.Restart()
	id := r.ID
	// What the hub does when an approval opens: the old engine handles
	// it on a goroutine of its own, as it did in the helper before exit.
	old.Observe(wire.Event{Kind: wire.EventApprovalOpen, SessionID: &id, Payload: wire.ApprovalOpenEvent{Approval: wire.Approval{RequestID: "r_late", SessionID: &id}}})
	time.Sleep(200 * time.Millisecond)
	cur := r.Cursor()
	prompt("after the restart")
	if !r.WaitHook(cur, 10*time.Second) {
		t.Fatal("the second prompt was not drawn")
	}
	if !r.DrainHooks(10 * time.Second) {
		t.Fatal("the hook readers did not finish")
	}
	raw, err := os.ReadFile(filepath.Join(r.TogoHome, "run", "hooks-"+r.ID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(raw, []byte("after the restart")); n != 1 {
		t.Fatalf("the prompt after the restart is stored %d times, want once:\n%s", n, raw)
	}
}
