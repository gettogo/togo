package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/wire"
)

var env = map[string]string{"TOGO_SESSION": "s_aaaaaaaaaa"}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMapFixtures(t *testing.T) {
	pct, cost := 8.0, 0.01234
	cases := []struct {
		file, event string
		want        wire.HookData
		kind        string
		text        string
	}{
		{"user_prompt_submit.json", "UserPromptSubmit", wire.HookData{}, local.EventPrompt, ""},
		{"pre_tool_use.json", "PreToolUse", wire.HookData{Tool: "Bash"}, local.EventTool, ""},
		{"post_tool_use.json", "PostToolUse", wire.HookData{Tool: "Write"}, local.EventToolDone, ""},
		{"permission_request.json", "PermissionRequest", wire.HookData{Tool: "Bash", Command: "rm -rf node_modules"}, local.EventPermission, ""},
		{"notification_permission.json", "Notification", wire.HookData{Message: "Claude needs your permission to use Bash"}, local.EventPermission, ""},
		{"notification_idle.json", "Notification", wire.HookData{Message: "Claude is waiting for your input"}, local.EventAttention, ""},
		{"stop.json", "Stop", wire.HookData{Message: "Done. All tests pass."}, local.EventStop, ""},
		{"session_start.json", "SessionStart", wire.HookData{Model: "claude-opus-5"}, local.EventStart, ""},
		{"statusline.json", Statusline, wire.HookData{Model: "Opus", ContextPercent: &pct, CostUSD: &cost}, local.EventMeta, "Opus 8%"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			ev, out, err := Map(c.event, fixture(t, c.file), env)
			if err != nil {
				t.Fatal(err)
			}
			c.want.AgentSessionID = "abc123"
			got, _ := json.Marshal(ev.Data)
			want, _ := json.Marshal(c.want)
			if ev.Event != c.kind || string(got) != string(want) || out.Text != c.text {
				t.Fatalf("event %q data %s text %q; want %q %s %q", ev.Event, got, out.Text, c.kind, want, c.text)
			}
			if ev.Agent != "claude" || ev.SessionID == nil || *ev.SessionID != "s_aaaaaaaaaa" || ev.Cwd == "" {
				t.Fatalf("envelope %+v", ev)
			}
		})
	}
}

func TestToolInputNeverInActivity(t *testing.T) {
	ev, _, err := Map("PreToolUse", fixture(t, "pre_tool_use.json"), env)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ev)
	if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "curl") {
		t.Fatalf("tool input leaked: %s", b)
	}
	_, _, err = Map("PreToolUse", []byte(`{"tool_input":"sk-ant-SECRET`), env)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error %v", err)
	}
}

func TestSessionFromEnvOnly(t *testing.T) {
	for _, e := range []map[string]string{{}, {"TOGO_SESSION": "work"}, {"TOGO_SESSION": "../x"}} {
		ev, _, err := Map("Stop", fixture(t, "stop.json"), e)
		if err != nil || ev.SessionID != nil {
			t.Fatalf("env %v: session %v err %v", e, ev.SessionID, err)
		}
	}
}

func TestUnsupportedEvent(t *testing.T) {
	if _, _, err := Map("Bogus", []byte(`{}`), env); err == nil {
		t.Fatal("unknown event accepted")
	}
}

func TestDecision(t *testing.T) {
	var out struct {
		HookSpecificOutput struct {
			HookEventName string `json:"hookEventName"`
			Decision      struct {
				Behavior string `json:"behavior"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	for answer, want := range map[string]string{"Allow": "allow", "Deny": "deny", "deny": "deny"} {
		if err := json.Unmarshal(Decision(answer), &out); err != nil {
			t.Fatal(err)
		}
		if out.HookSpecificOutput.HookEventName != "PermissionRequest" || out.HookSpecificOutput.Decision.Behavior != want {
			t.Fatalf("%s -> %+v", answer, out)
		}
	}
	if Decision("something") != nil {
		t.Fatal("unknown answer produced a decision")
	}
}

func TestAskForTitle(t *testing.T) {
	ev, _, _ := Map("PermissionRequest", fixture(t, "permission_request.json"), env)
	a := AskFor(ev, 540)
	if a.Title != "Allow Bash: rm -rf node_modules" || len(a.Options) != 2 || a.TimeoutSec != 540 {
		t.Fatalf("ask %+v", a)
	}
}
