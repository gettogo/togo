package claude

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// /effort saves the level in ~/.claude/settings.json for every session;
// Togo's effort change restarts the session with --effort instead and never
// types /effort.
func TestEffortIsSessionScoped(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	act := a.Actions["set_effort"]
	if act.Type != nil || len(act.Branch) != 3 {
		t.Fatalf("set_effort %+v", act)
	}
	for i, b := range act.Branch {
		if b.Relaunch == nil {
			t.Fatalf("branch %d has no relaunch", i)
		}
		argv := strings.Join(b.Relaunch.Argv, " ")
		resumes := strings.Contains(argv, "--resume {{.state.harness_session}}")
		// Only the last branch, for a session with no conversation yet,
		// starts fresh: there is nothing to resume.
		if !strings.Contains(argv, "--effort {{.value}}") || resumes == (i == len(act.Branch)-1) {
			t.Fatalf("argv %s", argv)
		}
	}
	// The first branch keeps the session's permission mode across the restart.
	if argv := strings.Join(act.Branch[0].Relaunch.Argv, " "); !strings.Contains(argv, "--permission-mode {{.state.mode}}") {
		t.Fatalf("mode not kept: %s", argv)
	}
	if act.Confirm == "" {
		t.Fatal("a restart needs a confirm")
	}
}

// /model saves the model as the default for every new session; Togo's
// model change restarts this session with --model instead, keeping its
// effort and mode, and never types /model.
func TestModelIsSessionScoped(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	act := a.Actions["set_model"]
	if act.Type != nil || len(act.Branch) != 2 || act.Confirm == "" {
		t.Fatalf("set_model %+v", act)
	}
	for i, b := range act.Branch {
		if b.Type != nil || b.Relaunch == nil {
			t.Fatalf("branch %d types or has no relaunch", i)
		}
		if argv := strings.Join(b.Relaunch.Argv, " "); !strings.Contains(argv, "--model {{.id}}") {
			t.Fatalf("argv %s", argv)
		}
		if len(b.Relaunch.Keep) != 2 {
			t.Fatalf("keep %v", b.Relaunch.Keep)
		}
	}
	if argv := strings.Join(act.Branch[0].Relaunch.Argv, " "); !strings.Contains(argv, "--resume {{.state.harness_session}}") {
		t.Fatalf("first branch does not resume: %s", argv)
	}
}

// "Don't ask" is not in Shift+Tab's cycle, so switching back to it from
// another mode restarts the session with --permission-mode dontAsk.
func TestDontAskIsARelaunch(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	act := a.Actions["set_mode"]
	if len(act.Branch) != 3 || act.Branch[0].Relaunch == nil || act.Branch[1].Relaunch == nil || act.Branch[2].Keys == nil || !act.Branch[2].Keys.Cycle {
		t.Fatalf("set_mode %+v", act)
	}
	if argv := strings.Join(act.Branch[0].Relaunch.Argv, " "); !strings.Contains(argv, "--permission-mode {{.id}}") ||
		!strings.Contains(act.Branch[0].When, `"dontAsk"`) {
		t.Fatalf("dontAsk branch %+v", act.Branch[0])
	}
}
