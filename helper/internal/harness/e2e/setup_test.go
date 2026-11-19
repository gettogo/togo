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
	return enginetest.New(t, enginetest.Options{Agent: agent, Home: e.home, Cwd: e.cwd, Created: e.start.Add(-30 * time.Second)})
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
//   - agent-file-mismatch, restart-moved-agent-step, restart-new-agent-step
//     and reread-count: TestKnownFault_SubagentFilesMatchedByArrival.
var known = []string{"agent-file-mismatch", "restart-moved-agent-step", "restart-new-agent-step", "reread-count"}

func check(t *testing.T, where string, st enginetest.State, l *sessiongen.Layout) {
	t.Helper()
	fail(t, where, enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: true}))
}

func fail(t *testing.T, where string, vs []enginetest.Violation) {
	t.Helper()
	enginetest.Fail(t, where, enginetest.Without(vs, known...))
}
