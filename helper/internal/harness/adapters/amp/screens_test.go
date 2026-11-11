package amp

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// testdata/screens are real captures of Amp 0.0.1794312042 (mode low,
// routed to OpenRouter GLM 5.3 Flash, a settings file with "ask Bash"):
// the "Approval Required" box at 80 and 47 columns, and the free-text box
// "Tell Amp what to do differently" that "Reject with feedback" opens.
// Amp has no question tool of its own (`amp tools list`), and its plugin
// API sees no approval, so the screen is the only source.
func screen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/screens/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The approval box: a "‣" cursor over four rows, moved with ↑/↓ and taken
// with Enter. "Reject with feedback" opens a text box, and Enter there
// rejects with none (checked live); "Allow All for Every Session" writes a
// permanent rule.
func TestApprovalScreens(t *testing.T) {
	for _, name := range []string{"real-perm-80", "real-perm-47"} {
		s := screen(t, name)
		if !strings.Contains(s, "Approval Required") || !strings.Contains(s, "‣ Allow Once") {
			t.Fatalf("%s: not the approval box", name)
		}
		p, ok := detect.Detect(s)
		if !ok || p.Kind != detect.KindPermission || p.Title != "Approval Required" {
			t.Fatalf("%s: %v %+v", name, ok, p)
		}
		if p.Subject == nil || p.Subject.Kind != wire.SubjectCommand || p.Subject.Text != "echo hello-from-amp" {
			t.Fatalf("%s: subject %+v", name, p.Subject)
		}
		want := []struct {
			label, role string
			keys        []string
		}{
			{"Allow Once", wire.RoleApprove, []string{"Enter"}},
			{"Reject with feedback", wire.RoleDeny, []string{"Down", "Enter", "Enter"}}, // Enter leaves the feedback empty
			{"Allow All for This Session", wire.RoleApproveAlways, []string{"Down", "Down", "Enter"}},
			{"Allow All for Every Session", wire.RoleApproveAlways, []string{"Down", "Down", "Down", "Enter"}},
		}
		if len(p.Options) != len(want) {
			t.Fatalf("%s: options %+v", name, p.Options)
		}
		for i, w := range want {
			o := p.Options[i]
			if o.Label != w.label || o.Role != w.role || !reflect.DeepEqual(p.Keys[o.N], w.keys) {
				t.Errorf("%s: option %d = %+v keys %v, want %+v", name, i, o, p.Keys[o.N], w)
			}
		}
	}
}
