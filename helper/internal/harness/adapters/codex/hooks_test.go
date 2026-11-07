package codex

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/rpc"
)

func hookRecs(payloads ...string) []harness.Record {
	out := make([]harness.Record, len(payloads))
	for i, p := range payloads {
		out[i] = harness.Record{Source: "hooks", At: t0.Add(time.Duration(i) * time.Second), Data: json.RawMessage(p)}
	}
	return out
}

var hookTurn = []string{
	`{"session_id":"thr-9","hook_event_name":"SessionStart","model":"gpt-5.5","cwd":"/w"}`,
	`{"session_id":"thr-9","turn_id":"t1","hook_event_name":"UserPromptSubmit","prompt":"fix the bug","future_field":{"x":1}}`,
	`{"session_id":"thr-9","turn_id":"t1","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":["bash","-lc","go test ./..."]},"tool_use_id":"call_1"}`,
	`{"session_id":"thr-9","turn_id":"t1","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"go test ./..."}}`,
	`{"session_id":"thr-9","turn_id":"t1","hook_event_name":"PostToolUse","tool_name":"Bash","tool_use_id":"call_1","tool_response":"ok  pkg 0.1s"}`,
	`{"session_id":"thr-9","turn_id":"t1","hook_event_name":"PreToolUse","tool_name":"apply_patch","tool_input":{"input":"*** Begin Patch"}}`,
	`{"session_id":"thr-9","turn_id":"t1","hook_event_name":"PostToolUse","tool_name":"apply_patch","tool_response":{"ok":true}}`,
	`{"session_id":"thr-9","turn_id":"t1","hook_event_name":"Stop","last_assistant_message":"Done. Tests pass."}`,
}

func TestHooksDrawWithoutAppServer(t *testing.T) {
	m := NewHooks(harness.Env{SessionID: "s_hooks_only"})
	out := run(m, hookRecs(hookTurn...))
	items := out.last()
	if u := items["t1:user"]; u.User == nil || u.User.Text != "fix the bug" {
		t.Fatalf("user %+v", u)
	}
	bash := items["call_1"].Tool
	if bash == nil || bash.Title != "go test ./..." || bash.State != harness.StateOK || bash.Output != "ok  pkg 0.1s" {
		t.Fatalf("bash %+v", bash)
	}
	var waited bool
	for _, x := range out.items {
		if x.ID == "call_1" && x.Tool.State == harness.StateWaiting {
			waited = true
		}
	}
	if !waited {
		t.Fatal("permission request did not mark the tool waiting")
	}
	var patch *harness.ToolBody
	for _, x := range items {
		if x.Tool != nil && x.Tool.Name == "apply_patch" {
			patch = x.Tool
		}
	}
	if patch == nil || patch.State != harness.StateOK || patch.Category != harness.ToolEdit {
		t.Fatalf("patch %+v", patch)
	}
	if r := items["t1:reply"]; r.Assistant == nil || r.Assistant.Text != "Done. Tests pass." {
		t.Fatalf("reply %+v", r)
	}
	if tu := items["t1"].Turn; tu == nil || tu.State != harness.StateOK || tu.Steps != 2 {
		t.Fatalf("turn %+v", tu)
	}
	st := m.State()
	if st["harness_session"] != "thr-9" || st["model"] != "gpt-5.5" || st["working"] != "false" {
		t.Fatalf("state %v", st)
	}
}

// With the app-server feed attached, hooks only keep state, so a turn is
// not drawn twice; once the feed's connection closes, hooks draw again.
func TestHooksQuietWhileAppServerLive(t *testing.T) {
	env := harness.Env{SessionID: "s_both"}
	app := NewAppServer(env)
	run(app, records(t, "appserver-turn.jsonl")[:1])
	hooks := NewHooks(env)
	out := run(hooks, hookRecs(hookTurn...))
	if len(out.items) != 0 {
		t.Fatalf("hooks drew %d items while the app-server is live", len(out.items))
	}
	if hooks.State()["harness_session"] != "thr-9" {
		t.Fatal("hooks state not kept")
	}
	app.Map(harness.Record{Data: json.RawMessage(`{"method":"` + rpc.MethodClosed + `"}`)})
	out = run(hooks, hookRecs(hookTurn[1]))
	if len(out.items) == 0 {
		t.Fatal("hooks stayed quiet after the app-server feed closed")
	}
}

func TestAnswerShapes(t *testing.T) {
	params := json.RawMessage(`{"questions":[{"id":"scope","header":"Scope","question":"?"},{"id":"other","question":"?"}]}`)
	res, err := answerUserInput(params, map[string]json.RawMessage{"answers": json.RawMessage(`{"scope":["Only Add"," "],"bogus":["x"]}`)})
	if err != nil || string(res) != `{"answers":{"scope":{"answers":["Only Add"]}}}` {
		t.Fatalf("user input: %s %v", res, err)
	}
	if _, err := answerUserInput(params, map[string]json.RawMessage{"answers": json.RawMessage(`{"bogus":["x"]}`)}); err == nil {
		t.Fatal("answer for no asked question accepted")
	}
}

// While the app-server is live it owns the turn. An interrupted turn ends
// with its turn/completed and no Stop hook, so the hooks' working, left
// set by the last PreToolUse, must not keep the session working; and
// turn/interrupt must name the app-server's turn, not the hooks' guess.
func TestHooksLeaveTurnToLiveAppServer(t *testing.T) {
	env := harness.Env{SessionID: "s_interrupt"}
	app := NewAppServer(env)
	run(app, records(t, "appserver-turn.jsonl")[:1])
	hooks := NewHooks(env)
	run(hooks, hookRecs(`{"session_id":"thr-9","hook_event_name":"UserPromptSubmit","prompt":"go"}`,
		`{"session_id":"thr-9","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"sleep 60"}}`))
	st := hooks.State()
	if _, ok := st["working"]; ok {
		t.Fatalf("hooks report working while the app-server is live: %v", st)
	}
	if _, ok := st["turn"]; ok {
		t.Fatalf("hooks report a turn while the app-server is live: %v", st)
	}
	app.Map(harness.Record{Data: json.RawMessage(`{"method":"` + rpc.MethodClosed + `"}`)})
	if hooks.State()["working"] != "true" {
		t.Fatal("hooks own working again once the app-server feed closed")
	}
}
