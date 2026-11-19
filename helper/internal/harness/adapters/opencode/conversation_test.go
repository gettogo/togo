package opencode

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// OpenCode 1.x and Kilo Code: the session the TUI shows names the
// conversation. A recorded session read twice (a reconnect after a helper
// restart) names the same one throughout; the TUI switching to another
// session moves it, and the engine starts the chat over on it.
func TestShownSessionNamesTheConversation(t *testing.T) {
	m := NewEvents(harness.Env{SessionID: "s_oc", Cwd: "/Users/me/proj"})
	var seen []string
	for range 2 {
		for _, r := range recorded(t) {
			m.Map(r)
			if c := m.State()[harness.StateConversation]; c != "" && (len(seen) == 0 || seen[len(seen)-1] != c) {
				seen = append(seen, c)
			}
		}
	}
	if len(seen) != 1 || seen[0] != recordedSession {
		t.Fatalf("conversations named %q, want only %q", seen, recordedSession)
	}
	m.Map(harness.Record{At: time.Now(), Data: json.RawMessage(`{"type":"tui.session.select","properties":{"sessionID":"ses_other"}}`)})
	if c := m.State()[harness.StateConversation]; c != "ses_other" {
		t.Fatalf("conversation %q after the TUI switched, want ses_other", c)
	}
}
