package engine

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/redact"
)

// Shell items carry the raw command and its output; both go through the
// redactor like tool items do.
func TestShellItemsAreRedacted(t *testing.T) {
	s := &sess{e: &Engine{red: redact.Redactor{}}, anchors: map[string]*harness.Anchor{"sh1": nil}}
	it := harness.Item{ID: "sh1", Kind: harness.KindShell, Shell: &harness.ShellBody{
		Command: "export ANTHROPIC_AUTH_TOKEN=plainsecret1",
		Output:  "OPENAI_API_KEY=plainsecret2\n" + strings.Repeat("x", harness.MaxToolOutput),
	}}
	s.prepareLocked(&it, nil)
	if strings.Contains(it.Shell.Command, "plainsecret") || !strings.Contains(it.Shell.Command, redact.Marker) {
		t.Errorf("command = %q", it.Shell.Command)
	}
	if len(it.Shell.Output) > harness.MaxToolOutput {
		t.Errorf("output not capped: %d", len(it.Shell.Output))
	}
	it2 := harness.Item{ID: "sh1", Kind: harness.KindShell, Shell: &harness.ShellBody{Output: "a\nOPENAI_API_KEY=plainsecret2\nb"}}
	s.prepareLocked(&it2, nil)
	if strings.Contains(it2.Shell.Output, "plainsecret") {
		t.Errorf("output = %q", it2.Shell.Output)
	}
}
