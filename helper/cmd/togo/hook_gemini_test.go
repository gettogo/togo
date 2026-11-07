package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/local"
)

func TestGeminiHookForwards(t *testing.T) {
	env := map[string]string{"TOGO_SESSION": "s_aaaaaaaaaa"}
	fw, err := geminiHook("AfterAgent", []byte(`{"session_id":"g1","cwd":"/w","hook_event_name":"AfterAgent","transcript_path":"/t.json"}`), env)
	if err != nil || fw.Event != local.EventStop || fw.Hook != "AfterAgent" || fw.Data.AgentSessionID != "g1" || fw.Cwd != "/w" || !strings.Contains(string(fw.Payload), "/t.json") {
		t.Fatalf("%+v %v", fw, err)
	}
	if _, err := geminiHook("Nope", nil, env); err == nil {
		t.Fatal("unknown event accepted")
	}
	if forwardAdapters["gemini"].blocks("BeforeTool") {
		t.Fatal("gemini hooks never block")
	}
	for _, agent := range []string{"gemini", "auggie"} {
		if !local.HookAgents[agent] {
			t.Errorf("%s hooks would be refused by the daemon", agent)
		}
	}
	// The whole forward is valid local.hook params.
	if _, err := json.Marshal(fw); err != nil {
		t.Fatal(err)
	}
}

func TestClipKeepsRunes(t *testing.T) {
	if got := clip("aé", 2); got != "a" {
		t.Fatalf("clip = %q", got)
	}
}

// Every agent `togo hook` forwards is one the daemon accepts.
func TestEveryHookAgentAccepted(t *testing.T) {
	for agent := range forwardAdapters {
		if !local.HookAgents[agent] {
			t.Errorf("forwarded agent %s is not in local.HookAgents", agent)
		}
	}
	for agent := range hookAdapters {
		if !local.HookAgents[agent] {
			t.Errorf("hook agent %s is not in local.HookAgents", agent)
		}
	}
}
