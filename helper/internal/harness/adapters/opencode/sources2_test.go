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

// Seen live on 2.3.0: /api/agent listed a custom primary agent
// (audit-observer) and set_mode took its id, but the mode chip offered only
// Build and Plan. The chip reads the agents source, which lists what the
// terminal's agent switch does: primary agents that are not hidden.
// testdata/v2/agents-custom.json is what the server listed (id, name, mode
// and hidden of each agent).
func TestModesListCustomPrimaryAgents(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	if a.Modes == nil || a.Modes.Source != "agents" {
		t.Fatalf("modes do not read the agents source: %+v", a.Modes)
	}
	var written []string
	for _, c := range a.Modes.Choices {
		written = append(written, c.ID)
	}
	if got := strings.Join(written, " "); got != "build plan" {
		t.Fatalf("choices served when the source is down: %s", got)
	}
	v, err := engine.JQFirst(a.Sources["agents"].Select, fixture2(t, "agents-custom.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range v.([]any) {
		ids = append(ids, m.(map[string]any)["id"].(string))
	}
	if got := strings.Join(ids, " "); got != "build plan audit-observer" {
		t.Fatalf("agents source lists %s", got)
	}
}
