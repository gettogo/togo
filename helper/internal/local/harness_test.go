package local

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestHookForwardsRawPayloadToHarness(t *testing.T) {
	hub := &fakeHub{}
	var got []wire.HookForward
	h := &Handlers{Hub: hub, Approvals: &fakeApprovals{}, Pub: &fakePub{},
		Harness: func(id string, f wire.HookForward) {
			if id != sid {
				t.Errorf("session = %s", id)
			}
			got = append(got, f)
		}}
	call := func(params string) error {
		_, err := h.Methods()[wire.MethodLocalHook](context.Background(), json.RawMessage(params))
		return err
	}
	if err := call(`{"agent":"claude","event":"prompt","sessionId":"` + sid + `","hook":"UserPromptSubmit","payload":{"hook_event_name":"UserPromptSubmit","prompt":"hi"}}`); err != nil {
		t.Fatal(err)
	}
	if hub.status != statusWorking || len(got) != 1 || got[0].Hook != "UserPromptSubmit" || !strings.Contains(string(got[0].Payload), `"prompt":"hi"`) {
		t.Fatalf("status %q, forwarded %+v", hub.status, got)
	}
	// A forward-only event changes nothing else.
	hub.status = ""
	if err := call(`{"agent":"claude","event":"harness","sessionId":"` + sid + `","hook":"SubagentStart","payload":{"agent_id":"a1"}}`); err != nil {
		t.Fatal(err)
	}
	if hub.status != "" || len(got) != 2 || got[1].Hook != "SubagentStart" {
		t.Fatalf("status %q, forwarded %+v", hub.status, got)
	}
	// Old helpers send no payload; the hook still reaches the harness.
	if err := call(`{"agent":"claude","event":"stop","sessionId":"` + sid + `"}`); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[2].Payload != nil || got[2].Event != EventStop {
		t.Fatalf("forwarded %+v", got[2])
	}
	big := `{"x":"` + strings.Repeat("a", wire.MaxHookPayload) + `"}`
	if err := call(`{"agent":"claude","event":"tool","sessionId":"` + sid + `","hook":"PreToolUse","payload":` + big + `}`); err != nil {
		t.Fatal(err)
	}
	if got[3].Payload != nil {
		t.Fatal("an oversized payload was forwarded")
	}
	// Outside a managed session nothing is forwarded.
	if err := call(`{"agent":"claude","event":"harness","payload":{}}`); err != nil || len(got) != 4 {
		t.Fatalf("unattributed hook forwarded: %v", err)
	}
}
