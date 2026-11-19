// Package e2e runs the real engine and adapters over generated sessions
// of real length and checks the engine-level rules on what a client sees.
package e2e

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

type env struct {
	home, cwd string
	start     time.Time
}

func newEnv(t *testing.T, age time.Duration) env {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(home, "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	return env{home: home, cwd: cwd, start: time.Now().Add(-age).UTC().Truncate(time.Second)}
}

func (e env) rig(t *testing.T, agent string) *enginetest.Rig {
	return e.rigWith(t, enginetest.Options{Agent: agent})
}

func (e env) rigWith(t *testing.T, o enginetest.Options) *enginetest.Rig {
	o.Home, o.Cwd, o.Created = e.home, e.cwd, e.start.Add(-30*time.Second)
	if o.ID == "" {
		o.ID = enginetest.NewID()
	}
	return enginetest.New(t, o)
}

// hasPrompt waits for the n-th generated prompt ("Step n: ...") to be in
// the log.
func hasPrompt(n int) func(enginetest.State) bool {
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
func cuts(seed int64, k, lo, hi int) []int {
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

func agentItems(st enginetest.State) map[string]harness.Item {
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
var known = []string{"agent-turn-moved"}

func check(t *testing.T, where string, st enginetest.State, l *sessiongen.Layout) {
	t.Helper()
	fail(t, where, enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: true}), l)
}

// fail reports the violations outside the known faults. A job row named
// after a subagent that a restart drops is reported as agent-shown-as-job,
// and a restart adding or losing a row no task has as
// agent-row-without-task, so the failure names the fault.
func fail(t *testing.T, where string, vs []enginetest.Violation, layout ...*sessiongen.Layout) {
	t.Helper()
	l := &sessiongen.Layout{}
	if len(layout) > 0 && layout[0] != nil {
		l = layout[0]
	}
	for i, v := range vs {
		if v.Rule == "restart-changed" && strings.HasPrefix(v.Detail, "agent-") && turnOnly(v.Detail) {
			vs[i].Rule = "agent-turn-moved"
		}
		if (v.Rule == "restart-new-item" || v.Rule == "restart-lost-item") && strings.HasPrefix(v.Detail, "agent-") && !taskRow(l, v.Detail) {
			vs[i].Rule = "agent-row-without-task"
		}
		for _, a := range l.Truth.Agents {
			if v.Rule == "restart-lost-item" && strings.HasPrefix(v.Detail, "job-"+a.Task+" ") {
				vs[i].Rule = "agent-shown-as-job"
			}
		}
	}
	enginetest.Fail(t, where, enginetest.Without(vs, known...))
}

// turnOnly is true for a restart-changed detail ("<id> was k|p|turn|state,
// is k|p|turn|state") where only the turn differs and one side has none.
func turnOnly(detail string) bool {
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
func taskRow(l *sessiongen.Layout, detail string) bool {
	for _, a := range l.Truth.Agents {
		if strings.HasPrefix(detail, "agent-"+a.Task+" ") {
			return true
		}
	}
	return false
}
