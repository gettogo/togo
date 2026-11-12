package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

type planMapper struct {
	harness.PlanMiss
}

func (*planMapper) Name() string                                      { return "fake" }
func (*planMapper) Map(harness.Record) ([]harness.Item, []harness.Op) { return nil, nil }
func (*planMapper) State() map[string]string                          { return nil }

// A todo call no mapper could read is logged once per session with the
// adapter and the agent's version, and nothing the agent wrote.
func TestUnrecognisedPlanLoggedOnce(t *testing.T) {
	var logs []string
	e := &Engine{cfg: Config{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }}}
	s := &sess{e: e, ad: &adapter{Adapter: &spec.Adapter{Adapter: "omp"}}, version: "18.19.0"}
	m := &planMapper{}
	s.warnPlanLocked(m)
	if len(logs) != 0 {
		t.Fatalf("logged before a miss: %v", logs)
	}
	m.NoteUnrecognisedPlan()
	s.warnPlanLocked(m)
	s.warnPlanLocked(m)
	if len(logs) != 1 || !strings.Contains(logs[0], "unrecognised todo format") || !strings.Contains(logs[0], "omp") || !strings.Contains(logs[0], "18.19.0") {
		t.Fatalf("logs = %v", logs)
	}
	s.warnPlanLocked(&noWatcher{})
}

type noWatcher struct{ planMapper }

func (*noWatcher) PlanUnrecognised() bool { return false }
