package hookstyle

import (
	"encoding/json"
	"testing"
)

// Copilot's camelCase keys the mapper reads: workingDirectory, agentName,
// a string error, and subagent hooks without an agentId.
func TestCopilotAliases(t *testing.T) {
	d, _ := For("copilot")
	read := func(event, payload string) map[string]any {
		t.Helper()
		_, out, err := Normalize(d, event, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		json.Unmarshal(out, &m)
		return m
	}
	m := read("sessionStart", `{"sessionId":"c1","workingDirectory":"/p"}`)
	if m["cwd"] != "/p" || m["session_id"] != "c1" {
		t.Errorf("session start %v", m)
	}
	m = read("errorOccurred", `{"error":"model overloaded"}`)
	if m["error_details"] != "model overloaded" {
		t.Errorf("string error %v", m)
	}
	m = read("errorOccurred", `{"error":{"message":"boom"}}`)
	if m["error_details"] != "boom" {
		t.Errorf("object error %v", m)
	}
	m = read("subagentStart", `{"agentName":"reviewer","toolCallId":"call_9"}`)
	if m["agent_type"] != "reviewer" || m["agent_id"] != "call_9" {
		t.Errorf("subagent %v", m)
	}
	m = read("subagentStop", `{"agentName":"reviewer"}`)
	if m["agent_id"] != "reviewer" {
		t.Errorf("subagent without a call id %v", m)
	}
}

// A dialect's Modes rename the harness's permission_mode values to the
// adapter's mode ids. Dialects without them keep their values.
func TestModeIDs(t *testing.T) {
	q := Dialect{Agent: "renamed", Modes: map[string]string{"auto-edit": "auto_edit"}}
	_, out, _ := Normalize(q, "UserPromptSubmit", []byte(`{"permission_mode":"auto-edit"}`))
	var m map[string]any
	json.Unmarshal(out, &m)
	if m["permission_mode"] != "auto_edit" {
		t.Errorf("renamed %v", m)
	}
	dr, _ := For("droid")
	_, out, _ = Normalize(dr, "UserPromptSubmit", []byte(`{"permission_mode":"auto-edit"}`))
	json.Unmarshal(out, &m)
	if m["permission_mode"] != "auto-edit" {
		t.Errorf("droid %v", m)
	}
}
