package auggie

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Auggie is terminal-only: no capability, no feed, no action, and the
// installed 1.x passes the version gate so the session is not flagged.
func TestTerminalOnly(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Capabilities) != 0 || len(a.Feeds) != 0 || len(a.Actions) != 0 {
		t.Fatalf("capabilities %v, feeds %d, actions %d", a.Capabilities, len(a.Feeds), len(a.Actions))
	}
	if a.Harness.Range != ">=0.37 <2" {
		t.Fatalf("range %q", a.Harness.Range)
	}
	var logs []string
	engine.New(engine.Config{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	for _, l := range logs {
		if strings.Contains(l, "auggie") {
			t.Fatal(l)
		}
	}
}

func screen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/screens/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The tool permission dialog (--permission bash:ask-user, pi's select),
// captured from Auggie 1.2.0 at 80 and 47 columns, reaches the phone
// through the shared watcher: Yes approves, No denies, picked with arrows.
func TestPermissionScreens(t *testing.T) {
	for _, name := range []string{"real-perm-80", "real-perm-47"} {
		p, ok := detect.Detect(screen(t, name))
		if !ok {
			t.Fatalf("%s: no prompt", name)
		}
		if len(p.Options) != 2 || p.Options[0].Label != "Yes" || p.Options[0].Role != wire.RoleApprove ||
			p.Options[1].Label != "No" || p.Options[1].Role != wire.RoleDeny {
			t.Fatalf("%s: options %+v", name, p.Options)
		}
		if !reflect.DeepEqual(p.Keys[1], []string{"Enter"}) || !reflect.DeepEqual(p.Keys[2], []string{"Down", "Enter"}) {
			t.Fatalf("%s: keys %v", name, p.Keys)
		}
	}
}

// ask-user's single-select dialog (1.2.0, drawn like Claude Code's) is a
// question the watcher offers, with both options and the digits that pick
// them.
func TestAskScreenSingle(t *testing.T) {
	for _, name := range []string{"real-ask-single-80", "real-ask-single-47", "real-ask-batch-q1"} {
		p, ok := detect.Detect(screen(t, name))
		if !ok || len(p.Options) < 2 || !strings.HasPrefix(p.Options[0].Label, "Red") || p.Options[1].Label != "Blue" {
			t.Fatalf("%s: %+v", name, p)
		}
	}
}
