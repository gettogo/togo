package claude

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// hookAt is one Claude Code hook payload as the hook feed delivers it.
func hookAt(event, at string) harness.Record {
	ts, _ := time.Parse(time.RFC3339Nano, at)
	data := fmt.Sprintf(`{"hook_event_name":%q,"session_id":"131c4d56-2b3c-49a8-a467-c469caefd46e","permission_mode":"default"}`, event)
	return harness.Record{Source: "hooks", At: ts, Data: json.RawMessage(data)}
}

// interleave feeds hooks and transcript records in the order they reached
// the helper and returns the items and ops the transcript produced.
func interleave(t *testing.T, h *hooks, tr *transcript, steps []any) run {
	t.Helper()
	r := run{final: map[string]harness.Item{}}
	for _, s := range steps {
		switch v := s.(type) {
		case harness.Record:
			h.Map(v)
		case []harness.Record:
			got := mapAll(tr, v)
			r.items = append(r.items, got.items...)
			r.ops = append(r.ops, got.ops...)
			for id, it := range got.final {
				r.final[id] = it
			}
		}
	}
	return r
}

// The owner's interrupt, recorded: a turn starts a background shell, then
// asks to run a Bash call and an Agent. The owner presses Interrupt at the
// permission prompt. Claude Code rejects both calls, writes the interrupt
// and the turn's duration, and sends no Stop hook. The turn ends as
// interrupted, the session is no longer working although the last hook
// said so, and the background shell stays listed as running.
func TestInterruptAtPermissionPromptEndsWorking(t *testing.T) {
	h, tr := newHooks(env()), newTranscript(env())
	recs := records(t, "transcript-interrupt.jsonl")
	r := interleave(t, h, tr, []any{
		hookAt("UserPromptSubmit", "2026-11-07T13:04:33.100Z"),
		recs[:1],
		hookAt("PreToolUse", "2026-11-07T13:04:33.950Z"),
		recs[1:2],
		hookAt("PostToolUse", "2026-11-07T13:04:34.450Z"),
		recs[2:4],
		hookAt("PreToolUse", "2026-11-07T13:04:36.900Z"),
		hookAt("PermissionRequest", "2026-11-07T13:04:36.950Z"),
		recs[4:5],
		hookAt("PreToolUse", "2026-11-07T13:04:39.500Z"),
		hookAt("PermissionRequest", "2026-11-07T13:04:39.550Z"),
		recs[5:],
	})
	if st := h.State(); st["working"] != "true" {
		t.Fatalf("hooks state = %v; the recorded hooks end on a PermissionRequest", st)
	}
	if harness.MergeWorking(h.State(), tr.State()) {
		t.Fatalf("still working after the interrupt: hooks %v transcript %v", h.State(), tr.State())
	}
	turn := r.final["u7.turn"].Turn
	if turn == nil || turn.State != harness.StateInterrupted || turn.Ended == nil {
		t.Fatalf("turn = %+v, want interrupted and ended", turn)
	}
	if n := r.final["i7"]; n.Notice == nil || n.Notice.Code != harness.NoticeInterrupted {
		t.Fatalf("interrupt notice = %+v", n)
	}
	bg := r.final["bg-bgdev1"].Background
	if bg == nil || bg.State != harness.StateRunning {
		t.Fatalf("background shell = %+v, want still running", bg)
	}

	// The next prompt works again, and its Stop ends it.
	h.Map(hookAt("UserPromptSubmit", "2026-11-07T13:05:00.000Z"))
	if !harness.MergeWorking(h.State(), tr.State()) {
		t.Fatal("a new prompt's hook must show working before its transcript line")
	}
	h.Map(hookAt("Stop", "2026-11-07T13:05:09.000Z"))
	if harness.MergeWorking(h.State(), tr.State()) {
		t.Fatal("Stop must end working")
	}
}

// A turn a task notification starts has no prompt line in the transcript;
// its hooks show it working until its Stop.
func TestHooksAfterTranscriptTurnShowWorking(t *testing.T) {
	h, tr := newHooks(env()), newTranscript(env())
	recs := records(t, "transcript-interrupt.jsonl")
	interleave(t, h, tr, []any{recs})
	h.Map(hookAt("PreToolUse", "2026-11-07T13:06:00.000Z"))
	if !harness.MergeWorking(h.State(), tr.State()) {
		t.Fatal("a hook after the transcript's last turn must show working")
	}
	h.Map(hookAt("Stop", "2026-11-07T13:06:05.000Z"))
	if harness.MergeWorking(h.State(), tr.State()) {
		t.Fatal("Stop must end working")
	}
}

// Stop ends the whole turn: Escape until idle, at most twice, with the
// chord that stops background agents after the first press (Escape does
// nothing at the idle prompt they keep busy).
func TestInterruptPressesUntilIdle(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	k := a.Actions[harness.ActionInterrupt].Keys
	if k == nil || k.Until != spec.UntilIdle || k.Max != 2 || strings.Join(k.Keys, " ") != "Escape" ||
		strings.Join(k.Then, " ") != "C-x C-k C-x C-k" {
		t.Fatalf("interrupt keys = %+v", k)
	}
}
