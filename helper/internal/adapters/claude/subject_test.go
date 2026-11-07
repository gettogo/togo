package claude

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Each Claude tool that can ask for permission names what it acts on.
func TestPermissionRequestSubjectPerTool(t *testing.T) {
	env := map[string]string{"TOGO_SESSION": "s_aaaaaaaaaa"}
	for _, c := range []struct {
		tool, input, kind, text string
	}{
		{"Bash", `{"command":"go test ./..."}`, wire.SubjectCommand, "go test ./..."},
		{"Edit", `{"file_path":"/w/a.go","old_string":"x","new_string":"y"}`, wire.SubjectFile, "/w/a.go"},
		{"NotebookEdit", `{"notebook_path":"/w/n.ipynb","new_source":"x"}`, wire.SubjectFile, "/w/n.ipynb"},
		{"WebFetch", `{"url":"https://example.com/x","prompt":"p"}`, wire.SubjectURL, "https://example.com/x"},
		{"WebSearch", `{"query":"swift live activity"}`, wire.SubjectText, "swift live activity"},
		{"Grep", `{"pattern":"TODO","path":"/w"}`, wire.SubjectText, "TODO"},
	} {
		t.Run(c.tool, func(t *testing.T) {
			raw := []byte(`{"session_id":"x","tool_name":"` + c.tool + `","tool_input":` + c.input + `}`)
			ev, _, err := Map("PermissionRequest", raw, env)
			if err != nil {
				t.Fatal(err)
			}
			if ev.Data.Subject == nil || ev.Data.Subject.Kind != c.kind || ev.Data.Subject.Text != c.text {
				t.Fatalf("subject %+v, want %s %q", ev.Data.Subject, c.kind, c.text)
			}
		})
	}
}
