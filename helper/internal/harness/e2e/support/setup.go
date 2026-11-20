// Package support is what the end-to-end packages of the suite share. The
// suite runs the real engine and adapters over generated sessions of real
// length and checks the engine-level rules on what a client sees. It is
// split by the way an agent feeds the chat, so each part is its own test
// binary: hookfed (hooks), filefed (files and databases), apifed (an HTTP
// API), codex (an app-server) and sessions (OMP and Claude Code, with
// their long sessions and limits).
package support

import (
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

type Env struct {
	Home, Cwd string
	Start     time.Time
}

func NewEnv(t *testing.T, age time.Duration) Env {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(home, "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	return Env{Home: home, Cwd: cwd, Start: time.Now().Add(-age).UTC().Truncate(time.Second)}
}

func (e Env) Rig(t *testing.T, agent string) *enginetest.Rig {
	return e.RigWith(t, enginetest.Options{Agent: agent})
}

func (e Env) RigWith(t *testing.T, o enginetest.Options) *enginetest.Rig {
	o.Home, o.Cwd, o.Created = e.Home, e.Cwd, e.Start.Add(-30*time.Second)
	if o.ID == "" {
		o.ID = enginetest.NewID()
	}
	return enginetest.New(t, o)
}

// hasPrompt waits for the n-th generated prompt ("Step n: ...") to be in
// the log.
func HasPrompt(n int) func(enginetest.State) bool {
	prefix := "Step " + strconv.Itoa(n) + ":"
	return func(st enginetest.State) bool {
		for _, it := range st.Items {
			if it.User != nil && it.Parent == "" && strings.HasPrefix(it.User.Text, prefix) {
				return true
			}
		}
		return false
	}
}

// cuts picks k distinct turn counts in [lo, hi), sorted, from the seed.
func Cuts(seed int64, k, lo, hi int) []int {
	r := rand.New(rand.NewSource(seed))
	seen := map[int]bool{}
	for len(seen) < k {
		seen[lo+r.Intn(hi-lo)] = true
	}
	out := make([]int, 0, k)
	for c := range seen {
		out = append(out, c)
	}
	sort.Ints(out)
	return out
}

func AgentItems(st enginetest.State) map[string]harness.Item {
	out := map[string]harness.Item{}
	for _, it := range st.Items {
		if it.Agent != nil {
			out[it.Agent.AgentID] = it
		}
	}
	return out
}

// known are the rules a scenario may break today, each through a fault
// that a TestKnownFault test documents:
//   - agent-turn-moved, a restart changing only an agent row's turn
//     from or to none: TestKnownFault_ClaudeAgentReadBeforeItsCall.
var Known = []string{"agent-turn-moved"}

func Check(t *testing.T, where string, st enginetest.State, l *sessiongen.Layout) {
	t.Helper()
	Fail(t, where, enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: true}), l)
}

// fail reports the violations outside the known faults. A job row named
// after a subagent that a restart drops is reported as agent-shown-as-job,
// and a restart adding or losing a row no task has as
// agent-row-without-task, so the failure names the fault.
func Fail(t *testing.T, where string, vs []enginetest.Violation, layout ...*sessiongen.Layout) {
	t.Helper()
	l := &sessiongen.Layout{}
	if len(layout) > 0 && layout[0] != nil {
		l = layout[0]
	}
	for i, v := range vs {
		if v.Rule == "restart-changed" && strings.HasPrefix(v.Detail, "agent-") && TurnOnly(v.Detail) {
			vs[i].Rule = "agent-turn-moved"
		}
		if (v.Rule == "restart-new-item" || v.Rule == "restart-lost-item") && strings.HasPrefix(v.Detail, "agent-") && !TaskRow(l, v.Detail) {
			vs[i].Rule = "agent-row-without-task"
		}
		for _, a := range l.Truth.Agents {
			if v.Rule == "restart-lost-item" && strings.HasPrefix(v.Detail, "job-"+a.Task+" ") {
				vs[i].Rule = "agent-shown-as-job"
			}
		}
	}
	enginetest.Fail(t, where, enginetest.Without(vs, Known...))
}

// turnOnly is true for a restart-changed detail ("<id> was k|p|turn|state,
// is k|p|turn|state") where only the turn differs and one side has none.
func TurnOnly(detail string) bool {
	_, rest, ok := strings.Cut(detail, " was ")
	if !ok {
		return false
	}
	was, is, ok := strings.Cut(rest, ", is ")
	if !ok {
		return false
	}
	a, b := strings.Split(was, "|"), strings.Split(is, "|")
	return len(a) == 4 && len(b) == 4 && a[0] == b[0] && a[1] == b[1] && a[3] == b[3] && (a[2] == "") != (b[2] == "")
}

// taskRow is true when a detail names the row of a task the script started.
func TaskRow(l *sessiongen.Layout, detail string) bool {
	for _, a := range l.Truth.Agents {
		if strings.HasPrefix(detail, "agent-"+a.Task+" ") {
			return true
		}
	}
	return false
}

// WithoutAskOfferCards drops, from a restart's violations, the loss of an
// approval card that was closed before the restart and belonged to no
// question: the engine's offer of a question answered while the offer
// opened, drawn on its own (TestKnownFault_AskOfferAnsweredWhileOpeningShowsAsApproval).
// A pending approval, or one a question owns, is still checked.
func WithoutAskOfferCards(vs []enginetest.Violation, before enginetest.State) []enginetest.Violation {
	cards := map[string]bool{}
	for _, it := range before.Items {
		if it.Approval != nil && it.Approval.Owner == "" && it.Approval.Resolved != "" {
			cards[it.ID] = true
		}
	}
	var out []enginetest.Violation
	for _, v := range vs {
		id, _, _ := strings.Cut(v.Detail, " ")
		if v.Rule == "restart-lost-item" && cards[id] {
			continue
		}
		out = append(out, v)
	}
	return out
}
