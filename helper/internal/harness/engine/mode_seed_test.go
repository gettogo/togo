package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func statusOf(e *Engine, id string) *harness.StatusBlock {
	s, err := e.served(context.Background(), id)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sec := s.mission.section(harness.SectionStatus); sec != nil && sec.Status != nil {
		c := *sec.Status
		return &c
	}
	return nil
}

// O5: the mode is known from the screen before any hook or status line,
// a hook's newer value wins, and the screen is read again when a turn
// ends (a Shift+Tab at the desk leaves no record).
func TestModeSeededFromScreenAndPublished(t *testing.T) {
	const id = "s_modeseed01"
	pane := &seqPane{mode: "b"}
	e := newSeq(t, id, pane, nil)
	waitFor(t, func() bool { st := statusOf(e, id); return st != nil && st.Mode == "b" })
	if d, _ := e.Describe(context.Background(), id); d.Mode != "b" {
		t.Fatalf("describe mode %q", d.Mode)
	}
	deliver(id, map[string]any{"state": map[string]string{"mode": "c", "working": "true"}})
	waitFor(t, func() bool { st := statusOf(e, id); return st != nil && st.Mode == "c" })
	pane.mu.Lock()
	pane.mode = "a"
	pane.mu.Unlock()
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { st := statusOf(e, id); return st != nil && st.Mode == "a" })
}

func TestPartialStatusKeepsMetersAndCost(t *testing.T) {
	const id = "s_modeseed02"
	e := newSeq(t, id, &seqPane{mode: "a"}, nil)
	cost, ctx1, ctx2 := 0.5, 10.0, 20.0
	deliver(id, map[string]any{"ops": []harness.Op{{Op: harness.OpStatus, Status: &harness.StatusBlock{Model: "Opus", CostUSD: &cost,
		ContextPercent: &ctx1, Meters: []harness.Meter{{ID: "five_hour", Label: "5 hour limit", Percent: 31}}}}}})
	waitFor(t, func() bool { st := statusOf(e, id); return st != nil && st.CostUSD != nil })
	deliver(id, map[string]any{"ops": []harness.Op{{Op: harness.OpStatus, Status: &harness.StatusBlock{ContextPercent: &ctx2}}}})
	waitFor(t, func() bool { st := statusOf(e, id); return st != nil && *st.ContextPercent == 20 })
	st := statusOf(e, id)
	if st.Model != "Opus" || *st.CostUSD != 0.5 || len(st.Meters) != 1 {
		t.Fatalf("status %+v", st)
	}
}

// A cycle that misses its target never leaves the session in a mode
// nobody picked: it presses on back to where it began.
func TestFailedCycleReturnsToTheOriginalMode(t *testing.T) {
	const id = "s_modeseed03"
	pane := &seqPane{mode: "a", next: map[string]string{"a": "b", "b": "a"}}
	e := newSeq(t, id, pane, nil)
	res, _ := setMode(e, id, "c")
	pane.mu.Lock()
	mode := pane.mode
	pane.mu.Unlock()
	if res.Result != harness.ResultFailed || mode != "a" || !strings.Contains(res.Message, "back in A") {
		t.Fatalf("res %+v mode %q", res, mode)
	}
}

// Seen live: the first read came before Claude drew its prompt box (or
// while its folder trust dialog covered it), so the mode stayed unknown
// until the first turn ended. The start-up read is retried.
func TestModeSeedRetriedAtStart(t *testing.T) {
	old := seedRetry
	seedRetry = 20 * time.Millisecond
	t.Cleanup(func() { seedRetry = old })
	const id = "s_modeseed09"
	pane := &seqPane{}
	e := newSeq(t, id, pane, nil)
	pane.mu.Lock()
	pane.mode = "b"
	pane.mu.Unlock()
	waitFor(t, func() bool { st := statusOf(e, id); return st != nil && st.Mode == "b" })
}
