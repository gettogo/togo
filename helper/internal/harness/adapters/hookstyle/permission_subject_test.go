package hookstyle

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Every hook-style harness names what its permission request is about, so
// the notification and Live Activity can show it.
func TestPermissionRequestSubjectPerHarness(t *testing.T) {
	env := map[string]string{"TOGO_SESSION": "s_aaaaaaaaaa"}
	for _, c := range []struct {
		agent, event, payload string
		kind, text            string
	}{
		{"droid", "PermissionRequest", `{"tool_name":"Execute","tool_input":{"command":"make"}}`, wire.SubjectCommand, "make"},
		{"droid", "PermissionRequest", `{"tool_name":"Create","tool_input":{"file_path":"/w/new.ts"}}`, wire.SubjectFile, "/w/new.ts"},
		{"copilot", "permissionRequest", `{"toolName":"edit","toolArgs":"{\"path\":\"/w/b.rs\"}"}`, wire.SubjectFile, "/w/b.rs"},
		{"copilot", "permissionRequest", `{"toolName":"bash","toolArgs":"{\"command\":\"ls -la\"}"}`, wire.SubjectCommand, "ls -la"},
	} {
		t.Run(c.agent+" "+c.text, func(t *testing.T) {
			d, ok := For(c.agent)
			if !ok {
				t.Fatalf("no dialect %s", c.agent)
			}
			f, err := Reduce(d, c.event, []byte(c.payload), env)
			if err != nil {
				t.Fatal(err)
			}
			if f.Data.Subject == nil || f.Data.Subject.Kind != c.kind || f.Data.Subject.Text != c.text {
				t.Fatalf("subject %+v, want %s %q", f.Data.Subject, c.kind, c.text)
			}
		})
	}
}
