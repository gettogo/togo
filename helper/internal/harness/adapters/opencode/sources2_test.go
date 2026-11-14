package opencode

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// fixture2 decodes a recorded 2.x API answer from testdata/v2.
func fixture2(t *testing.T, name string) any {
	t.Helper()
	b, err := os.ReadFile("testdata/v2/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// Seen live on 2.3.0: an archived session stayed in the resume picker. The
// terminal's list asks for archived=false, so the picker asks the same, and
// drops an archived entry should the server send one anyway.
// testdata/v2/sessions-archived.json is the first three sessions a real
// /api/session answered, the second one archived (folder names replaced).
func TestResumePickerLeavesOutArchivedSessions(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	src := a.Sources["sessions"]
	path, err := engine.Render(src.HTTP.Path, map[string]any{"cwd": "/w/project"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "&archived=false") {
		t.Errorf("sessions path %s does not ask for archived=false", path)
	}
	v, err := engine.JQFirst(src.Select, fixture2(t, "sessions-archived.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, s := range v.([]any) {
		titles = append(titles, s.(map[string]any)["title"].(string))
	}
	if got := strings.Join(titles, "|"); got != "Audit fixture API create probe|Audit fixture recent 55" {
		t.Fatalf("picker lists %s", got)
	}
}
