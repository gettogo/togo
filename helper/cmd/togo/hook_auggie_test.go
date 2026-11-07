package main

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/local"
)

func TestAuggieHook(t *testing.T) {
	env := map[string]string{"TOGO_SESSION": "s_aaaaaaaaaa"}
	ev, _, err := auggieHook(context.Background(), "PreToolUse",
		[]byte(`{"hook_event_name":"PreToolUse","conversation_id":"c1","workspace_roots":["/w"],"tool_name":"view"}`), env)
	if err != nil || ev.Event != local.EventTool || ev.Data.Tool != "view" || ev.Cwd != "/w" || ev.SessionID == nil || ev.Data.AgentSessionID != "c1" {
		t.Fatalf("%+v %v", ev, err)
	}
	if _, _, err := auggieHook(context.Background(), "Nope", nil, env); err == nil {
		t.Fatal("unknown event accepted")
	}
	f, ok := forwardAdapters["auggie"]
	if !ok || f.blocks("PreToolUse") {
		t.Fatal("auggie must forward and never block")
	}
	fw, err := f.reduce("Stop", []byte(`{"hook_event_name":"Stop","conversation_id":"c1"}`), env)
	if err != nil || fw.Hook != "Stop" || len(fw.Payload) == 0 || fw.Event != local.EventStop {
		t.Fatalf("%+v %v", fw, err)
	}
}
