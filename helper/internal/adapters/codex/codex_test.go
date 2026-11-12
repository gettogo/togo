package codex

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMapFixtures(t *testing.T) {
	cases := []struct {
		file, event, want, tool, command, message string
	}{
		{"user_prompt_submit.json", "UserPromptSubmit", "prompt", "", "", ""},
		{"permission_request.json", "PermissionRequest", "permission", "Bash", "rm -rf build", ""},
		{"stop.json", "Stop", "stop", "", "", "Done. Tests pass."},
		{"notify_turn_complete.json", "notify", "stop", "", "", "Done."},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			a := &Adapter{}
			ev, err := a.Map(c.event, strings.NewReader(fixture(t, c.file)), map[string]string{"TOGO_SESSION": "s_abc"})
			if err != nil {
				t.Fatal(err)
			}
			if ev.Agent != "codex" || ev.Event != c.want || ev.Cwd != "/work/app" {
				t.Fatalf("event = %+v", ev)
			}
			if ev.SessionID == nil || *ev.SessionID != "s_abc" {
				t.Fatalf("session = %v", ev.SessionID)
			}
			d := ev.Data
			if d.Tool != c.tool || d.Command != c.command || d.Message != c.message || d.AgentSessionID != "0199-aaaa" {
				t.Fatalf("data = %+v", d)
			}
		})
	}
}

// A Codex the user started in a terminal has no TOGO_SESSION. Its hooks
// must never be attributed to a Togo Codex session in the same folder, even
// when that folder matches exactly one of them.
func TestHookWithoutTogoSessionIsDropped(t *testing.T) {
	var logged string
	a := &Adapter{
		Logf: func(f string, args ...any) { logged = f },
	}
	for _, c := range []struct{ event, file string }{{"Stop", "stop.json"}, {"PermissionRequest", "permission_request.json"}} {
		_, err := a.Map(c.event, strings.NewReader(fixture(t, c.file)), nil)
		if !errors.Is(err, ErrDropped) || logged == "" {
			t.Fatalf("%s: err = %v, logged = %q", c.event, err, logged)
		}
	}
}

func TestAgentSessionIdRemembered(t *testing.T) {
	a := &Adapter{}
	env := map[string]string{"TOGO_SESSION": "s_one"}
	if _, err := a.Map("UserPromptSubmit", strings.NewReader(fixture(t, "user_prompt_submit.json")), env); err != nil {
		t.Fatal(err)
	}
	ev, err := a.Map("Stop", strings.NewReader(fixture(t, "stop.json")), nil)
	if err != nil || *ev.SessionID != "s_one" {
		t.Fatalf("ev = %+v, err = %v", ev, err)
	}
}

func TestMalformedAndUnsupportedInputRejected(t *testing.T) {
	a := &Adapter{}
	env := map[string]string{"TOGO_SESSION": "s_abc"}
	for name, c := range map[string]struct{ event, in string }{
		"not json":      {"Stop", "{{{"},
		"array":         {"Stop", "[]"},
		"empty":         {"Stop", ""},
		"unknown event": {"SubagentStart", "{}"},
		"notify type":   {"notify", `{"type":"other"}`},
		"oversize":      {"Stop", `{"cwd":"` + strings.Repeat("a", 2<<20) + `"}`},
	} {
		if _, err := a.Map(c.event, strings.NewReader(c.in), env); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLongFieldsTruncatedAndErrorsOmitPayload(t *testing.T) {
	a := &Adapter{}
	in := `{"session_id":"x","cwd":"/w","last_assistant_message":"` + strings.Repeat("é", 1000) + `\u0007"}`
	ev, err := a.Map("Stop", strings.NewReader(in), map[string]string{"TOGO_SESSION": "s_a"})
	if err != nil || len([]rune(ev.Data.Message)) != maxMessageRune {
		t.Fatalf("err = %v, len = %d", err, len([]rune(ev.Data.Message)))
	}
	_, err = a.Map("Stop", strings.NewReader("secret-token-123"), nil)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestPermissionSubjectKindFromTool(t *testing.T) {
	patch := `*** Begin Patch\n*** Update File: src/lib.rs\n@@\n-a\n+b\n*** Add File: docs/Заметки.md\n+x\n*** End Patch\n`
	cases := []struct {
		name, tool, input string
		want              *wire.Subject
	}{
		{"shell argv", "exec_command", `{"command":["git","status"]}`, &wire.Subject{Kind: wire.SubjectCommand, Text: "git status"}},
		{"bash string", "Bash", `{"command":"cargo   test"}`, &wire.Subject{Kind: wire.SubjectCommand, Text: "cargo test"}},
		{"patch argv", "apply_patch", `{"command":["apply_patch","` + patch + `"]}`, &wire.Subject{Kind: wire.SubjectFile, Text: "src/lib.rs\ndocs/Заметки.md"}},
		{"patch string", "apply_patch", `{"command":"` + patch + `"}`, &wire.Subject{Kind: wire.SubjectFile, Text: "src/lib.rs\ndocs/Заметки.md"}},
		{"patch input", "apply_patch", `{"input":"` + patch + `"}`, &wire.Subject{Kind: wire.SubjectFile, Text: "src/lib.rs\ndocs/Заметки.md"}},
		{"file path", "Write", `{"file_path":"README.md"}`, &wire.Subject{Kind: wire.SubjectFile, Text: "README.md"}},
		{"other tool", "web_search", `{"command":"weather"}`, &wire.Subject{Kind: wire.SubjectText, Text: "weather"}},
		{"nothing to show", "Bash", `{}`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := `{"session_id":"0199-aaaa","cwd":"/work/app","tool_name":"` + c.tool + `","tool_input":` + c.input + `}`
			ev, err := (&Adapter{}).Map("PermissionRequest", strings.NewReader(in), map[string]string{"TOGO_SESSION": "s_abc"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(ev.Data.Command, "Begin Patch") {
				t.Fatalf("patch text in command %q", ev.Data.Command)
			}
			got := ev.Data.Subject
			switch {
			case c.want == nil && got != nil:
				t.Fatalf("subject = %+v, want none", *got)
			case c.want != nil && (got == nil || *got != *c.want):
				t.Fatalf("subject = %+v, want %+v", got, *c.want)
			}
			if got != nil {
				if err := got.Validate(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestToolAndStartEvents(t *testing.T) {
	a := &Adapter{}
	env := map[string]string{"TOGO_SESSION": "s_abcdefghij"}
	for event, want := range map[string]string{"SessionStart": "start", "PreToolUse": "tool", "PostToolUse": "tool_done"} {
		ev, err := a.Map(event, strings.NewReader(`{"session_id":"x","cwd":"/w","tool_name":"shell"}`), env)
		if err != nil || ev.Event != want {
			t.Fatalf("%s: %+v %v", event, ev, err)
		}
		if want != "start" && ev.Data.Tool != "shell" {
			t.Errorf("%s: tool %q", event, ev.Data.Tool)
		}
	}
}
