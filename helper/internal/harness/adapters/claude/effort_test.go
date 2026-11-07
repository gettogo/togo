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
	if act.Type != nil || len(act.Branch) != 2 {
		t.Fatalf("set_effort %+v", act)
	}
	for i, b := range act.Branch {
		if b.Relaunch == nil {
			t.Fatalf("branch %d has no relaunch", i)
		}
		argv := strings.Join(b.Relaunch.Argv, " ")
		if !strings.Contains(argv, "--effort {{.value}}") || !strings.Contains(argv, "--resume {{.state.harness_session}}") {
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
