package support

import (
	"os"
	"os/exec"
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
func LongMode() bool { return os.Getenv("TOGO_E2E_LONG") == "1" }

// scale returns the default or the long-mode value.
func Scale(def, long int) int {
	if LongMode() {
		return long
	}
	return def
}

// settleUntil waits for the n-th generated prompt.
func SettleUntil(r *enginetest.Rig, n int) enginetest.State {
	return r.SettleUntil(DriveLimit, HasPrompt(n))
}

// drive writes the turns in pieces ending at points and the last turn,
// checking the rules after each piece, and restarts the engine after each
// piece but the last, comparing the state before and after. The first
// points[0] turns must already be written to l and the rig started.
// It returns the state after the last piece.
func Drive(t *testing.T, r *enginetest.Rig, l *sessiongen.Layout, turns []sessiongen.Turn, points []int) enginetest.State {
	t.Helper()
	st := SettleUntil(r, points[0])
	CheckEdge(t, "start", st, l)
	done := points[0]
	for i, p := range append(append([]int(nil), points[1:]...), len(turns)) {
		if err := l.Append(turns[done:p]...); err != nil {
			t.Fatal(err)
		}
		done = p
		live := SettleUntil(r, p)
		CheckEdge(t, "live "+strconv.Itoa(p), live, l)
		st = live
		if i == len(points)-1 {
			break
		}
		r.Restart()
		after := SettleUntil(r, p)
		CheckEdge(t, "restart "+strconv.Itoa(p), after, l)
		Fail(t, "restart "+strconv.Itoa(p), Restarted(live, after))
		st = after
	}
	return st
}

// restoreTwice restarts the engine twice over the finished files: both
// restores hold what the files hold and agree with each other.
func RestoreTwice(t *testing.T, r *enginetest.Rig, l *sessiongen.Layout, prompts int) enginetest.State {
	t.Helper()
	r.Restart()
	first := SettleUntil(r, prompts)
	CheckEdge(t, "first restore", first, l)
	r.Restart()
	second := SettleUntil(r, prompts)
	Fail(t, "second restore", enginetest.Reread(first, second))
	Fail(t, "second restore (rules)", Restarted(first, second))
	return second
}

// items returns the items of a state (newest copy of each id) that pass f.
func Items(st enginetest.State, f func(harness.Item) bool) []harness.Item {
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

func OfKind(kind string) func(harness.Item) bool {
	return func(it harness.Item) bool { return it.Kind == kind }
}

func Notice(code string) func(harness.Item) bool {
	return func(it harness.Item) bool { return it.Notice != nil && it.Notice.Code == code }
}

// userPrompts returns the main-conversation prompts of the state.
func UserPrompts(st enginetest.State) []harness.Item {
	return Items(st, func(it harness.Item) bool { return it.User != nil && it.Parent == "" })
}

func TurnsIn(st enginetest.State, state string) []harness.Item {
	return Items(st, func(it harness.Item) bool { return it.Turn != nil && it.Turn.State == state })
}

// statusOf is the status block of the mission control: model, mode,
// effort.
func StatusOf(st enginetest.State) harness.StatusBlock {
	for _, s := range st.Mission.Sections {
		if s.Kind == harness.SectionStatus && s.Status != nil {
			return *s.Status
		}
	}
	return harness.StatusBlock{}
}

// quick is a settle for a short scenario.
const Quick = 15 * time.Second

// restarted is enginetest.Restarted, except that when the restart's replay
// window began inside a turn (the log of a long file holds the last 2000
// records) the items of the turn at the window's edge are not compared:
// their turn is gone or invented, see TestKnownFault_PiReplayWindowStartsATurnMidTurn
// and TestKnownFault_KimiReplayWindowLeavesItemsWithoutATurn.
func Restarted(before, after enginetest.State) []enginetest.Violation {
	vs := enginetest.Restarted(before, after)
	edge := EdgeItems(before, after)
	if len(edge) == 0 {
		return vs
	}
	var out []enginetest.Violation
	for _, v := range vs {
		skip := false
		for id := range edge {
			skip = skip || strings.Contains(v.Detail, id)
		}
		if !skip {
			out = append(out, v)
		}
	}
	return out
}

// edgeItems are the ids of the items of after that lie before its first
// whole turn, when the replay dropped older main-conversation items that
// before held, and the id of that first turn if it has no prompt.
func EdgeItems(before, after enginetest.State) map[string]bool {
	oldest := func(st enginetest.State) (time.Time, bool) {
		var o time.Time
		found := false
		for _, it := range Items(st, func(it harness.Item) bool { return it.Parent == "" && it.Turn == nil }) {
			if !found || it.At.Before(o) {
				o, found = it.At, true
			}
		}
		return o, found
	}
	b, ok1 := oldest(before)
	a, ok2 := oldest(after)
	if !ok1 || !ok2 || !a.After(b) {
		return nil
	}
	prompted := map[string]bool{}
	for _, it := range UserPrompts(after) {
		prompted[it.TurnID] = true
	}
	edge := map[string]bool{}
	var firstWhole time.Time
	for _, it := range Items(after, func(it harness.Item) bool { return it.Turn != nil && prompted[it.ID] }) {
		if firstWhole.IsZero() || it.At.Before(firstWhole) {
			firstWhole = it.At
		}
	}
	for _, it := range Items(after, func(harness.Item) bool { return true }) {
		if it.Turn != nil && !prompted[it.ID] || !firstWhole.IsZero() && it.At.Before(firstWhole) {
			edge[it.ID] = true
		}
	}
	return edge
}

// procsOf is the process view of a rig: the engine then attributes files
// by who holds them open, as it does with a real pane. Agents whose files
// are shared by a folder (pi, kimi) need it to follow a /new.
//
// The pane's process is a child that holds no file, as Pi and Kimi Code
// hold none (they append and close per entry). It is not this test
// process: the engine under test runs here and holds the files it tails,
// so naming this process made the agent seem to hold whatever the engine
// had open at the moment it looked, and a /new was followed or not by
// timing.
func ProcsOf(t *testing.T) func() []int {
	t.Helper()
	cmd := exec.Command("sleep", "600")
	if err := cmd.Start(); err != nil {
		t.Fatalf("pane process: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := cmd.Process.Pid
	return func() []int { return []int{pid} }
}

// rigMod is e.rig with its options changed by mod.
func (e Env) RigMod(t *testing.T, agent string, mod func(*enginetest.Options)) *enginetest.Rig {
	opt := enginetest.Options{Agent: agent, Home: e.Home, Cwd: e.Cwd, Created: e.Start.Add(-30 * time.Second)}
	if mod != nil {
		mod(&opt)
	}
	if opt.ID == "" {
		opt.ID = enginetest.NewID()
	}
	return enginetest.New(t, opt)
}

// now moves the generator's clock to the present, for a file the agent
// writes after the engine started: the engine takes a new file only when
// it was written since it began to watch.
func Now(l *sessiongen.Layout) {
	if d := time.Since(l.Now()); d > 0 {
		l.Skip(d)
	}
}

// never reports whether the state never satisfies ok within d.
func Never(r *enginetest.Rig, d time.Duration, ok func(enginetest.State) bool) bool {
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
func WaitFor(t *testing.T, r *enginetest.Rig, what string, ok func(enginetest.State) bool) enginetest.State {
	t.Helper()
	deadline := time.Now().Add(DriveLimit)
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
func ExitNotices(vs []enginetest.Violation) []enginetest.Violation {
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
const DriveLimit = 30 * time.Second

// checkEdge is check, except that when the log has dropped its oldest items
// (a conversation longer than its 2000 items) a turn whose prompt was
// dropped may start before its oldest remaining item: the turn is kept up to
// date by every later record while its first items were dropped.
func CheckEdge(t *testing.T, where string, st enginetest.State, l *sessiongen.Layout) {
	t.Helper()
	vs := enginetest.Without(enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: true}), Known...)
	if st.Discarded > 0 {
		prompted := map[string]bool{}
		for _, it := range UserPrompts(st) {
			prompted[it.TurnID] = true
		}
		var keep []enginetest.Violation
		for _, v := range vs {
			id, _, _ := strings.Cut(v.Detail, " ")
			if v.Rule == "turn-start-outside" && !prompted[id] {
				continue
			}
			keep = append(keep, v)
		}
		vs = keep
	}
	enginetest.Fail(t, where, vs)
}

// offeredAsks drops, from a restart's violations, the approval items the
// engine made when it offered an open question to the phone as an ask
// (engine/feedapproval.go offerLocked; the item names the question as its
// owner, engine/builtin.go addApproval). The daemon keeps approvals in
// memory only (daemon/compose.go builds approval.New with no file, and
// the approval package writes none), so after a restart that link is
// gone in the daemon too; the question item it pointed at is restored
// from the agent's files and keeps its own state.
func OfferedAsks(before enginetest.State, vs []enginetest.Violation) []enginetest.Violation {
	owned := map[string]bool{}
	for _, it := range before.Items {
		if it.Approval != nil && it.Approval.Owner != "" {
			owned[it.ID] = true
		}
	}
	var out []enginetest.Violation
	for _, v := range vs {
		id, _, _ := strings.Cut(v.Detail, " ")
		if v.Rule == "restart-lost-item" && owned[id] {
			continue
		}
		out = append(out, v)
	}
	return out
}
