package e2e

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// longMode is TOGO_E2E_LONG=1: the scenarios that cross the engine's file
// read limits (8 MiB, 2000 records) at their full size. The default size
// crosses the record limit with small records and runs in seconds.
func longMode() bool { return os.Getenv("TOGO_E2E_LONG") == "1" }

// scale returns the default or the long-mode value.
func scale(def, long int) int {
	if longMode() {
		return long
	}
	return def
}

// settleUntil waits for the n-th generated prompt.
func settleUntil(r *enginetest.Rig, n int) enginetest.State {
	return r.SettleUntil(driveLimit, hasPrompt(n))
}

// drive writes the turns in pieces ending at points and the last turn,
// checking the rules after each piece, and restarts the engine after each
// piece but the last, comparing the state before and after. The first
// points[0] turns must already be written to l and the rig started.
// It returns the state after the last piece.
func drive(t *testing.T, r *enginetest.Rig, l *sessiongen.Layout, turns []sessiongen.Turn, points []int) enginetest.State {
	t.Helper()
	st := settleUntil(r, points[0])
	check(t, "start", st, l)
	done := points[0]
	for i, p := range append(append([]int(nil), points[1:]...), len(turns)) {
		if err := l.Append(turns[done:p]...); err != nil {
			t.Fatal(err)
		}
		done = p
		live := settleUntil(r, p)
		check(t, "live "+strconv.Itoa(p), live, l)
		st = live
		if i == len(points)-1 {
			break
		}
		r.Restart()
		after := settleUntil(r, p)
		check(t, "restart "+strconv.Itoa(p), after, l)
		fail(t, "restart "+strconv.Itoa(p), restarted(live, after))
		st = after
	}
	return st
}

// restoreTwice restarts the engine twice over the finished files: both
// restores hold what the files hold and agree with each other.
func restoreTwice(t *testing.T, r *enginetest.Rig, l *sessiongen.Layout, prompts int) enginetest.State {
	t.Helper()
	r.Restart()
	first := settleUntil(r, prompts)
	check(t, "first restore", first, l)
	r.Restart()
	second := settleUntil(r, prompts)
	fail(t, "second restore", enginetest.Reread(first, second))
	fail(t, "second restore (rules)", restarted(first, second))
	return second
}

// items returns the items of a state (newest copy of each id) that pass f.
func items(st enginetest.State, f func(harness.Item) bool) []harness.Item {
	seen := map[string]harness.Item{}
	for _, it := range st.Items {
		if old, ok := seen[it.ID]; !ok || it.Seq > old.Seq {
			seen[it.ID] = it
		}
	}
	var out []harness.Item
	for _, it := range st.Items {
		if c, ok := seen[it.ID]; ok && c.Seq == it.Seq && f(it) {
			out = append(out, it)
		}
	}
	return out
}

func ofKind(kind string) func(harness.Item) bool {
	return func(it harness.Item) bool { return it.Kind == kind }
}

func notice(code string) func(harness.Item) bool {
	return func(it harness.Item) bool { return it.Notice != nil && it.Notice.Code == code }
}

// userPrompts returns the main-conversation prompts of the state.
func userPrompts(st enginetest.State) []harness.Item {
	return items(st, func(it harness.Item) bool { return it.User != nil && it.Parent == "" })
}

func turnsIn(st enginetest.State, state string) []harness.Item {
	return items(st, func(it harness.Item) bool { return it.Turn != nil && it.Turn.State == state })
}

// statusOf is the status block of the mission control: model, mode,
// effort.
func statusOf(st enginetest.State) harness.StatusBlock {
	for _, s := range st.Mission.Sections {
		if s.Kind == harness.SectionStatus && s.Status != nil {
			return *s.Status
		}
	}
	return harness.StatusBlock{}
}

// quick is a settle for a short scenario.
const quick = 15 * time.Second

// restarted is enginetest.Restarted, except that when the restart's replay
// window began inside a turn (the log of a long file holds the last 2000
// records) the turn at the window's edge is not compared: see
// TestKnownFault_PiReplayWindowStartsATurnMidTurn.
func restarted(before, after enginetest.State) []enginetest.Violation {
	vs := enginetest.Restarted(before, after)
	edge := edgeTurn(before, after)
	if edge == "" {
		return vs
	}
	var out []enginetest.Violation
	for _, v := range vs {
		if !strings.Contains(v.Detail, edge) {
			out = append(out, v)
		}
	}
	return out
}

// edgeTurn is the oldest turn of after when the replay dropped older
// main-conversation items that before held, else "".
func edgeTurn(before, after enginetest.State) string {
	oldest := func(st enginetest.State) (harness.Item, bool) {
		var o harness.Item
		found := false
		for _, it := range items(st, func(it harness.Item) bool { return it.Turn != nil }) {
			if !found || it.At.Before(o.At) {
				o, found = it, true
			}
		}
		return o, found
	}
	b, ok1 := oldest(before)
	a, ok2 := oldest(after)
	if !ok1 || !ok2 || !a.At.After(b.At) {
		return ""
	}
	return a.ID
}

// procsOf is the process view of a rig whose agent is this test process:
// the engine then attributes files by who holds them open, as it does
// with a real pane. Agents whose files are shared by a folder (pi, kimi)
// need it to follow a /new.
func procsOf() func() []int { return func() []int { return []int{os.Getpid()} } }

// rigWith is e.rig with options.
func (e env) rigWith(t *testing.T, agent string, mod func(*enginetest.Options)) *enginetest.Rig {
	opt := enginetest.Options{Agent: agent, Home: e.home, Cwd: e.cwd, Created: e.start.Add(-30 * time.Second)}
	if mod != nil {
		mod(&opt)
	}
	return enginetest.New(t, opt)
}

// now moves the generator's clock to the present, for a file the agent
// writes after the engine started: the engine takes a new file only when
// it was written since it began to watch.
func now(l *sessiongen.Layout) {
	if d := time.Since(l.Now()); d > 0 {
		l.Skip(d)
	}
}

// never reports whether the state never satisfies ok within d.
func never(r *enginetest.Rig, d time.Duration, ok func(enginetest.State) bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok(r.Read()) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
	return true
}

// waitFor waits for ok on a read of the session.
func waitFor(t *testing.T, r *enginetest.Rig, what string, ok func(enginetest.State) bool) enginetest.State {
	t.Helper()
	deadline := time.Now().Add(driveLimit)
	for {
		st := r.Read()
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("never saw %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// exitNotices drops the violations about the notice the engine writes
// itself when the hub reports the agent gone (engine/sess.go
// exitNoticeLocked): it is not in any file, carries no turn, and is not
// there after a restart once the agent runs again.
func exitNotices(vs []enginetest.Violation) []enginetest.Violation {
	var out []enginetest.Violation
	for _, v := range vs {
		if strings.Contains(v.Detail, "exit-") && v.Rule == "restart-lost-item" {
			continue
		}
		out = append(out, v)
	}
	return out
}

// driveLimit bounds each wait: the engine reads a large append to a
// followed file at its own pace.
const driveLimit = 30 * time.Second
