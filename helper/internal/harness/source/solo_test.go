package source

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Alone is yes only on evidence: a process view that lists no other agent
// of the kind in the folder. No view, a failed lookup or another agent
// there is no. The answer is reused for a while.
func TestSoloAlone(t *testing.T) {
	procs := &fakeProcs{tree: []int{100}}
	env := harness.Env{Agent: "goose", Cwd: "/w/proj", Procs: procs}
	if !(&Solo{}).Alone(env) {
		t.Fatal("alone in the folder: no")
	}
	procs.others = map[string]bool{"/w/proj": true}
	if (&Solo{}).Alone(env) {
		t.Fatal("another agent in the folder: yes")
	}
	procs.others, procs.err = nil, context.DeadlineExceeded
	if (&Solo{}).Alone(env) {
		t.Fatal("failed lookup: yes")
	}
	if (&Solo{}).Alone(harness.Env{Agent: "goose", Cwd: "/w/proj"}) {
		t.Fatal("no process view: yes")
	}
	procs.err = nil
	s := &Solo{}
	s.Alone(env)
	procs.others = map[string]bool{"/w/proj": true}
	if !s.Alone(env) {
		t.Fatal("the answer was not reused")
	}
}
