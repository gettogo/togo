package claude

import (
	"context"
	"strings"
	"testing"
	"time"
)

// gateProcs counts n Claude Code processes in the session's tree, once
// gate is closed.
type gateProcs struct {
	n    int
	gate chan struct{}
}

func (gateProcs) Tree(context.Context) []int { return []int{1} }
func (gateProcs) Holders(context.Context, []string) (map[string][]int, error) {
	return nil, nil
}
func (gateProcs) Others(context.Context, []string, string) (bool, error) { return false, nil }
func (g gateProcs) Within(ctx context.Context, _ []string) (int, error) {
	select {
	case <-g.gate:
		return g.n, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// The agent died with no SessionEnd (killed, or it exited while the
// helper was down) and the helper restarted. The next startup of another
// session is held, with the hooks of that session after it, while the
// pane's processes are counted off the session's lock. With one Claude
// Code in the pane (the one that started) they are all mapped in order;
// with the followed one still running above it (a nested run) they are
// all dropped.
func TestStartupHeldUntilTheProcessCheck(t *testing.T) {
	for _, c := range []struct {
		running int
		own     bool
	}{{1, true}, {2, false}} {
		e, first, second := followSetup(t)
		sessionStart(t, newHooks(e), "startup", first)
		g := gateProcs{n: c.running, gate: make(chan struct{})}
		e.Procs = g
		h := newHooks(e) // the helper restarted
		woke := make(chan struct{}, 1)
		h.Wake(func() { woke <- struct{}{} })

		id := strings.TrimSuffix(second[strings.LastIndex(second, "/")+1:], ".jsonl")
		sessionStart(t, h, "startup", second)
		var mapped []string
		for _, ev := range []string{"UserPromptSubmit", "PreToolUse", "TaskCreated"} {
			items, ops := h.Map(hookRecord(t, `{"hook_event_name":"`+ev+`","session_id":"`+id+`","task_id":"t1","task_subject":"Plan"}`))
			if len(items)+len(ops) > 0 {
				mapped = append(mapped, ev)
			}
		}
		if got := h.State(); got["transcript_path"] != first || got["working"] != "" || len(mapped) > 0 {
			t.Fatalf("%d running: payloads mapped before the check ended: %v %v", c.running, got, mapped)
		}
		if items, ops := h.Woken(); len(items)+len(ops) > 0 {
			t.Fatal("mapped while the check runs")
		}
		close(g.gate)
		select {
		case <-woke:
		case <-time.After(30 * time.Second):
			t.Fatal("the check never woke the engine")
		}
		// One more payload after the check, before the engine reads it.
		h.Map(hookRecord(t, `{"hook_event_name":"Stop","session_id":"`+id+`"}`))
		_, ops := h.Woken()
		st := h.State()
		if c.own {
			if st["transcript_path"] != second || st["working"] != "false" || len(ops) != 1 || ops[0].Row == nil || ops[0].Row.ID != "t1" {
				t.Fatalf("one Claude Code: state %v, ops %+v; want the new session, the task, and the Stop last", st, ops)
			}
			continue
		}
		if st["transcript_path"] != first || st["working"] != "" || len(ops) != 0 {
			t.Fatalf("nested run: state %v, ops %+v; want all dropped", st, ops)
		}
		h.Map(hookRecord(t, `{"hook_event_name":"UserPromptSubmit","session_id":"`+id+`"}`))
		if h.State()["working"] != "" {
			t.Fatal("a later payload of the dropped run was mapped")
		}
	}
}
