package enginetest

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Violation is one broken rule.
type Violation struct {
	Rule   string
	Detail string
}

func (v Violation) String() string { return v.Rule + ": " + v.Detail }

// Facts is what the test knows beyond the engine's state.
type Facts struct {
	Truth sessiongen.Truth
	// Over is true when the script's last turn has ended: nothing the
	// agent started is still at work.
	Over bool
}

// Check runs every rule that needs one state and returns what broke.
func Check(st State, f Facts) []Violation {
	var out []Violation
	out = append(out, Ended(st, f)...)
	out = append(out, Working(st)...)
	out = append(out, Paging(st)...)
	out = append(out, Answered(st, f)...)
	out = append(out, TurnTimes(st)...)
	return out
}

// latest is each item's newest copy by id; the log keeps one already, so
// a second copy is itself a fault Paging reports.
func latest(items []harness.Item) map[string]harness.Item {
	m := map[string]harness.Item{}
	for _, it := range items {
		if old, ok := m[it.ID]; !ok || it.Seq > old.Seq {
			m[it.ID] = it
		}
	}
	return m
}

func running(state string) bool {
	return state == harness.StateRunning || state == harness.StateWaiting
}

// Ended: no agent or background item or row is running when its own file
// recorded its exit, the main conversation reported its end, or the agent
// itself exited.
func Ended(st State, f Facts) []Violation {
	var out []Violation
	over := map[string]string{}
	for _, a := range f.Truth.Agents {
		switch {
		case a.Exited:
			over[a.Task] = "its file records its exit"
		case a.Settled:
			over[a.Task] = "the conversation reported its end"
		}
	}
	jobs := map[string]bool{}
	for _, j := range f.Truth.Jobs {
		jobs[j] = true
	}
	ids := make([]string, 0)
	items := latest(st.Items)
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		it := items[id]
		if it.Agent != nil && running(it.Agent.State) {
			if why, ok := over[it.Agent.AgentID]; ok {
				out = append(out, Violation{"agent-running-after-end", fmt.Sprintf("%s is %s but %s", id, it.Agent.State, why)})
			} else if f.Truth.Exited {
				out = append(out, Violation{"agent-running-after-exit", id + " is running after the agent exited"})
			}
		}
		if it.Background != nil && running(it.Background.State) && (jobs[it.Background.JobID] || f.Truth.Exited) {
			out = append(out, Violation{"job-running-after-end", id + " is running but the job settled"})
		}
	}
	for _, sec := range st.Mission.Sections {
		if sec.Kind != harness.SectionAgents && sec.Kind != harness.SectionBackground {
			continue
		}
		for _, row := range sec.Rows {
			if !running(row.State) {
				continue
			}
			it, ok := items[row.ID]
			switch {
			case f.Truth.Exited:
				out = append(out, Violation{"row-running-after-exit", row.ID + " row is running after the agent exited"})
			case ok && it.Agent != nil && over[it.Agent.AgentID] != "":
				out = append(out, Violation{"row-running-after-end", row.ID + " row is running but " + over[it.Agent.AgentID]})
			case ok && it.Background != nil && jobs[it.Background.JobID]:
				out = append(out, Violation{"row-running-after-end", row.ID + " row is running but the job settled"})
			}
		}
	}
	return out
}

// Working: the summary says working only while a turn or an agent runs.
func Working(st State) []Violation {
	if !st.Mission.Summary.Working {
		return nil
	}
	for _, it := range latest(st.Items) {
		if it.Turn != nil && running(it.Turn.State) || it.Agent != nil && running(it.Agent.State) {
			return nil
		}
	}
	return []Violation{{"working-with-nothing-running", "the summary says working, and no turn or agent runs"}}
}

// Paging: no id twice, Seq rising across pages, and every page but the
// oldest holds the asked number of main-conversation items, unless the
// page's byte limit cut it.
func Paging(st State) []Violation {
	var out []Violation
	seen := map[string]int64{}
	var prev int64
	for _, it := range st.Items {
		if s, ok := seen[it.ID]; ok {
			out = append(out, Violation{"duplicate-id", fmt.Sprintf("%s at seq %d and %d", it.ID, s, it.Seq)})
		}
		seen[it.ID] = it.Seq
		if it.Seq <= prev {
			out = append(out, Violation{"seq-order", fmt.Sprintf("%s has seq %d after %d", it.ID, it.Seq, prev)})
		}
		prev = it.Seq
	}
	for i, p := range st.Pages {
		if i == 0 && !st.More[0] {
			continue // the oldest page: the log ends there
		}
		if i == 0 {
			out = append(out, Violation{"page-more", "the oldest page read still says older items exist"})
			continue
		}
		main, size := 0, 0
		for _, it := range p {
			if it.Parent == "" {
				main++
			}
			size += len(fmt.Sprint(it))
		}
		if main != st.Limit && size < 500<<10 {
			out = append(out, Violation{"short-page", fmt.Sprintf("page %d of %d holds %d main items, asked %d", i, len(st.Pages), main, st.Limit)})
		}
	}
	return out
}

// SamePaging: two reads of one log with different page sizes hold the
// same items, so no page skipped or repeated one.
func SamePaging(a, b State) []Violation {
	key := func(st State) string {
		var ids []string
		for _, it := range st.Items {
			ids = append(ids, fmt.Sprintf("%s@%d", it.ID, it.Seq))
		}
		return strings.Join(ids, " ")
	}
	if key(a) != key(b) {
		return []Violation{{"paging-differs", fmt.Sprintf("pages of %d hold %d items, pages of %d hold %d", a.Limit, len(a.Items), b.Limit, len(b.Items))}}
	}
	return nil
}

// Answered: every prompt of a finished turn has a reply, a notice or a
// request in its turn.
func Answered(st State, f Facts) []Violation {
	items := latest(st.Items)
	answered := map[string]bool{}
	turnState := map[string]string{}
	for _, it := range items {
		if it.Parent != "" {
			continue
		}
		switch {
		case it.Assistant != nil, it.Notice != nil, it.Question != nil, it.Approval != nil:
			answered[it.TurnID] = true
		case it.Turn != nil:
			turnState[it.ID] = it.Turn.State
		}
	}
	var out []Violation
	for _, it := range st.Items {
		if it.User == nil || it.Parent != "" || it.TurnID == "" {
			continue
		}
		if running(turnState[it.TurnID]) && !f.Over {
			continue
		}
		if !answered[it.TurnID] {
			out = append(out, Violation{"prompt-unanswered", fmt.Sprintf("%s (turn %s, %s) has no reply, notice or request", it.ID, it.TurnID, turnState[it.TurnID])})
		}
	}
	return out
}

// TurnTimes: a turn starts within its own records: not before the
// earliest main-conversation item of the turn, nor after its latest.
func TurnTimes(st State) []Violation {
	lo, hi := map[string]time.Time{}, map[string]time.Time{}
	for _, it := range st.Items {
		if it.Parent != "" || it.TurnID == "" || it.Turn != nil || it.Agent != nil {
			continue
		}
		if t, ok := lo[it.TurnID]; !ok || it.At.Before(t) {
			lo[it.TurnID] = it.At
		}
		if it.At.After(hi[it.TurnID]) {
			hi[it.TurnID] = it.At
		}
	}
	var out []Violation
	for _, it := range latest(st.Items) {
		if it.Turn == nil {
			continue
		}
		l, ok := lo[it.ID]
		if !ok {
			continue
		}
		if s := it.Turn.Started; s.Before(l.Add(-time.Second)) || s.After(hi[it.ID].Add(time.Second)) {
			out = append(out, Violation{"turn-start-outside", fmt.Sprintf("%s started %s, its records run %s to %s", it.ID,
				s.Format(time.RFC3339), l.Format(time.RFC3339), hi[it.ID].Format(time.RFC3339))})
		}
	}
	return out
}

// view is what a restart may not change about an item.
func view(it harness.Item) string {
	state := ""
	switch {
	case it.Turn != nil:
		state = it.Turn.State
	case it.Agent != nil:
		state = it.Agent.State
	case it.Tool != nil:
		state = it.Tool.State
	case it.Background != nil:
		state = it.Background.State
	}
	return strings.Join([]string{it.Kind, it.Parent, it.TurnID, state}, "|")
}

// Restarted compares the state before a restart with the state after:
// the items both hold have the same kind, parent, turn and state, the
// later one holds nothing new, an item only the earlier one holds is older
// than everything the later one has (a restore reads a long file's recent
// part only), and the mission summary is the same.
func Restarted(before, after State) []Violation {
	var out []Violation
	b, a := latest(before.Items), latest(after.Items)
	var oldest time.Time
	for _, it := range a {
		if it.Parent == "" && (oldest.IsZero() || it.At.Before(oldest)) {
			oldest = it.At
		}
	}
	for _, id := range sortedKeys(a) {
		ait := a[id]
		bit, ok := b[id]
		if !ok {
			out = append(out, Violation{"restart-new-item", id + " (" + view(ait) + ") appears only after the restart"})
			continue
		}
		if view(ait) != view(bit) {
			out = append(out, Violation{"restart-changed", fmt.Sprintf("%s was %s, is %s", id, view(bit), view(ait))})
		}
	}
	for _, id := range sortedKeys(b) {
		bit := b[id]
		if _, ok := a[id]; !ok && bit.Parent == "" && !bit.At.Before(oldest) {
			out = append(out, Violation{"restart-lost-item", id + " (" + view(bit) + ") is gone after the restart"})
		}
	}
	if bs, as := summary(before.Mission.Summary), summary(after.Mission.Summary); bs != as {
		out = append(out, Violation{"restart-summary", "summary was " + bs + ", is " + as})
	}
	return out
}

// Reread compares two restores of the same files: they must agree
// exactly, apart from seq and epoch.
func Reread(first, second State) []Violation {
	out := Restarted(first, second)
	if len(latest(first.Items)) != len(latest(second.Items)) {
		out = append(out, Violation{"reread-count", fmt.Sprintf("%d items, then %d", len(first.Items), len(second.Items))})
	}
	return out
}

func summary(s harness.Summary) string {
	plan := "none"
	if s.Plan != nil {
		plan = fmt.Sprintf("%d/%d", s.Plan.Done, s.Plan.Total)
	}
	return fmt.Sprintf("working=%v agents=%d background=%d needs=%d plan=%s", s.Working, s.Agents, s.Background, s.NeedsYou, plan)
}

func sortedKeys(m map[string]harness.Item) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Fail reports every violation on t.
func Fail(t interface {
	Helper()
	Errorf(string, ...any)
}, where string, vs []Violation) {
	t.Helper()
	for _, v := range vs {
		t.Errorf("%s: %s", where, v)
	}
}
