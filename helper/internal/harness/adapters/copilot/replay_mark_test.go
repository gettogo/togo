package copilot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

type marked struct {
	rec       harness.Record
	pre, post json.RawMessage
}

func hookRec(at time.Time, v map[string]any) harness.Record {
	v["session_id"] = testSession
	b, _ := json.Marshal(v)
	return harness.Record{Source: "hooks", At: at, Data: b}
}

func viewItems(items []harness.Item) string {
	var out []string
	for _, it := range items {
		s := it.ID + "|" + it.Kind
		if it.Assistant != nil {
			s += "|" + it.Assistant.Text
		}
		if it.Tool != nil {
			s += "|" + it.Tool.State
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n")
}

// Hook records replayed after a helper restart, with the marks kept when
// they were first mapped, give the same items: each reads the replies its
// first mapping read, though the log has grown since, and nothing waits.
func TestReplayedHooksReadTheRepliesTheyFirstRead(t *testing.T) {
	env, path := eventsFile(t)
	newMapper := func() harness.Mapper {
		f, _ := harness.LookupMapper(MapperHooks)
		return f(env)
	}
	appendLines(t, path, evUser)
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	live := newMapper()
	mk := live.(harness.ReplayMarker)
	var recs []marked
	var first []harness.Item
	step := func(rec harness.Record, log ...string) {
		appendLines(t, path, log...)
		pre := mk.ReplayMark()
		items, _ := live.Map(rec)
		first = append(first, items...)
		recs = append(recs, marked{rec, pre, mk.ReplayMark()})
	}
	step(hookRec(at, map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": "Add a header"}))
	step(hookRec(at.Add(time.Second), map[string]any{"hook_event_name": "PreToolUse", "tool_name": "bash", "tool_use_id": "toolu_1"}), evToolMsg)
	step(hookRec(at.Add(2*time.Second), map[string]any{"hook_event_name": "PostToolUse", "tool_name": "bash", "tool_use_id": "toolu_1"}))
	step(hookRec(at.Add(3*time.Second), map[string]any{"hook_event_name": "PreToolUse", "tool_name": "bash", "tool_use_id": "toolu_2"}), evNarrate)
	step(hookRec(at.Add(4*time.Second), map[string]any{"hook_event_name": "PostToolUse", "tool_name": "bash", "tool_use_id": "toolu_2"}))
	step(hookRec(at.Add(5*time.Second), map[string]any{"hook_event_name": "Stop"}), evFinal, evEnd)
	// A call whose message was never written: the first mapping waited
	// for it in vain.
	step(hookRec(at.Add(6*time.Second), map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": "Again"}))
	step(hookRec(at.Add(7*time.Second), map[string]any{"hook_event_name": "PreToolUse", "tool_name": "bash", "tool_use_id": "toolu_9"}))
	if !strings.Contains(viewItems(first), "Middleware goes first.") || !strings.Contains(viewItems(first), "X-Request-Id") {
		t.Fatalf("the live mapping read no replies:\n%s", viewItems(first))
	}

	// The log goes on after the restart point.
	appendLines(t, path, strings.Replace(evNarrate, "Middleware goes first.", "Written after the restart.", 1))
	replay := newMapper()
	rk := replay.(harness.ReplayMarker)
	var again []harness.Item
	start := time.Now()
	for _, r := range recs {
		rk.Replaying(r.pre, r.post)
		items, _ := replay.Map(r.rec)
		again = append(again, items...)
	}
	if took := time.Since(start); took > 300*time.Millisecond {
		t.Errorf("the replay took %v: it waited for replies", took)
	}
	if viewItems(again) != viewItems(first) {
		t.Fatalf("replayed:\n%s\nfirst mapped:\n%s", viewItems(again), viewItems(first))
	}
	if string(rk.ReplayMark()) != string(mk.ReplayMark()) {
		t.Fatalf("the replay ends at %s, the live mapper at %s", rk.ReplayMark(), mk.ReplayMark())
	}
	// Live again: the message written after the restart is read next.
	items, _ := replay.Map(hookRec(at.Add(8*time.Second), map[string]any{"hook_event_name": "Stop"}))
	if !strings.Contains(viewItems(items), "Written after the restart.") {
		t.Fatalf("after the replay: %s", viewItems(items))
	}
}

func TestRepliesUpToALimit(t *testing.T) {
	env, path := eventsFile(t)
	appendLines(t, path, evUser, evToolMsg)
	_, mid := replies(env, testSession, -1)
	appendLines(t, path, evNarrate, evFinal)
	if got, end := repliesUpTo(env, testSession, -1, mid); len(got) != 0 || end != mid {
		t.Fatalf("end under a limit: %d", end)
	}
	if got, end := repliesUpTo(env, testSession, 0, mid); len(got) != 1 || end != mid {
		t.Fatalf("read under a limit: %+v, %d", got, end)
	}
	if got, _ := repliesUpTo(env, testSession, mid, -1); len(got) != 2 {
		t.Fatalf("no limit: %+v", got)
	}
}
