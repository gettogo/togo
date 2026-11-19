package e2e

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// TestOpenCode2Settings: from the phone, a model and then one of its
// variants (the recorded model list), a custom agent as the mode (the
// recorded agent list), MCP servers disconnected and connected (the list
// as recorded before and after), a server command, compaction and a
// provider key are each the agent's API call carrying the pick; the
// server's answer (session.model.selected, session.agent.selected) is
// what the status and the pickers then show. The terminal's own commands
// are offered and typed. A reload's command.updated makes the command
// list be read again. An undo staged at the desk shows in the chat.
func TestOpenCode2Settings(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "opencode2/capability/model", "opencode2/capability/mode", "opencode2/capability/mcp",
		"opencode2/capability/commands", "opencode2/capability/compact", "opencode2/capability/settings", "opencode2/capability/status")
	s := oc2Start(t, 77, nil, sessiongen.Turn{Say: 20, Tools: 1})
	settleUntil(s.r, 1)
	sess := s.p.Session()
	act := func(name, target string, args map[string]any) {
		t.Helper()
		if res, err := s.r.Act(name, target, args); err != nil || res.Result == harness.ResultFailed {
			t.Fatalf("%s: %+v %v", name, res, err)
		}
	}

	// Model, then a variant of it.
	scr, err := s.r.Screen("model", nil)
	if err != nil || scr.Picker == nil {
		t.Fatalf("model screen: %+v %v", scr, err)
	}
	ref := ""
	for _, it := range scr.Picker.Items {
		if it.Title == "Inkling 2 Small Free" {
			ref = it.ID
		}
	}
	if ref == "" {
		t.Fatalf("the recorded model is not offered: %+v", scr.Picker.Items)
	}
	act("set_model", "", map[string]any{"id": ref})
	s.waitPost(t, "/api/session/"+sess+"/model")
	waitFor(t, s.r, "the new model in the status", func(st enginetest.State) bool {
		return strings.Contains(statusOf(st).Model, "inkling-2-small-free")
	})
	vs, err := s.r.Screen("variant", nil)
	if err != nil || vs.Picker == nil || len(vs.Picker.Items) != 7 || vs.Picker.Items[0].Title != "Default" {
		t.Fatalf("variant screen: %+v %v", vs.Picker, err)
	}
	high := vs.Picker.Items[5]
	act("set_model", "", map[string]any{"id": high.ID})
	c := s.waitPost(t, "/api/session/"+sess+"/model")
	eventually(t, 5*time.Second, "the variant's call", func() bool { return len(s.posts("/api/session/"+sess+"/model")) == 2 })
	c = s.posts("/api/session/" + sess + "/model")[1]
	var body struct {
		Model map[string]string `json:"model"`
	}
	if json.Unmarshal([]byte(c.Body), &body) != nil || body.Model["variant"] != "high" {
		t.Errorf("variant call body %s", c.Body)
	}
	eventually(t, 5*time.Second, "the variant marked current", func() bool {
		vs, err := s.r.Screen("variant", nil)
		return err == nil && vs.Picker != nil && vs.Picker.Selected == high.ID
	})

	// A custom agent as the mode.
	d := s.r.Describe()
	var modes []string
	for _, m := range d.Modes {
		modes = append(modes, m.ID)
	}
	if !slices.Contains(modes, "audit-observer") {
		t.Fatalf("modes %v lack the custom agent", modes)
	}
	act("set_mode", "", map[string]any{"id": "audit-observer"})
	if c := s.waitPost(t, "/api/session/"+sess+"/agent"); !strings.Contains(c.Body, `"audit-observer"`) {
		t.Errorf("mode call body %s", c.Body)
	}
	eventually(t, 5*time.Second, "the custom mode current", func() bool { return s.r.Describe().Mode == "audit-observer" })

	// MCP servers: disconnect one, connect another.
	mcp := func() map[string][]string {
		sc, err := s.r.Screen("mcp", nil)
		if err != nil || sc.LiveList == nil {
			t.Fatalf("mcp screen: %+v %v", sc, err)
		}
		out := map[string][]string{}
		for _, r := range sc.LiveList.Rows {
			for _, a := range r.Actions {
				out[r.ID] = append(out[r.ID], a.ID)
			}
		}
		return out
	}
	if m := mcp(); !slices.Equal(m["fixture_tools"], []string{"mcp_disconnect"}) || !slices.Equal(m["off_tools"], []string{"mcp_connect"}) {
		t.Fatalf("mcp rows %v", m)
	}
	act("mcp_disconnect", "fixture_tools", nil)
	act("mcp_connect", "off_tools", nil)
	eventually(t, 5*time.Second, "the mcp list after the changes", func() bool {
		m := mcp()
		return slices.Equal(m["fixture_tools"], []string{"mcp_connect"}) && slices.Equal(m["off_tools"], []string{"mcp_disconnect"})
	})

	// Commands: the terminal's own are offered; a server command goes to
	// its route; a reload makes the list be read again.
	comp, err := s.r.Complete(harness.CompleteCommand, "")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, it := range comp.Items {
		names = append(names, strings.TrimSpace(it.Insert))
	}
	for _, want := range []string{"/new", "/connect", "/variants", "/mcps"} {
		if !slices.Contains(names, want) && !slices.Contains(names, want+" ") {
			t.Errorf("the / menu lacks %s: %v", want, names)
		}
	}
	reads := func() int {
		n := 0
		for _, c := range s.api.Calls() {
			if c.Method == "GET" && strings.HasPrefix(c.Path, "/api/command") {
				n++
			}
		}
		return n
	}
	before := reads()
	s.do(t, func() error { return s.p.Event(s.l, "command.updated", true, map[string]any{}) })
	eventually(t, 5*time.Second, "the commands read again after the reload", func() bool {
		_, _ = s.r.Complete(harness.CompleteCommand, "")
		return reads() > before
	})
	if _, err := s.r.Send("/init", harness.SendNow); err != nil {
		t.Fatalf("send /init: %v", err)
	}
	if c := s.waitPost(t, "/api/session/"+sess+"/command"); !strings.Contains(c.Body, `"init"`) {
		t.Errorf("command body %s", c.Body)
	}
	if slices.Contains(s.r.Term.Submitted(), "/init") {
		t.Error("a server command was typed into the terminal")
	}

	act("compact", "", nil)
	s.waitPost(t, "/api/session/"+sess+"/compact")
	act("set_provider_key", "openai", map[string]any{"value": "sk-test-0000"})
	if c := s.waitPost(t, "/api/integration/openai/connect/key"); !strings.Contains(c.Body, "sk-test-0000") || c.Status != 204 {
		t.Errorf("key call %+v", c)
	}

	// An undo at the desk (revert.sse): a notice in the chat; a redo
	// rewrites it.
	var user string
	for _, it := range userPrompts(s.r.Read()) {
		user = it.ID
	}
	s.do(t, func() error {
		return s.p.Event(s.l, "session.revert.staged", true, map[string]any{"sessionID": sess,
			"revert": map[string]any{"messageID": user, "snapshot": "e60c60ddc4a2e67610cc8c55ec00df94a537da67", "files": []any{}}})
	})
	waitFor(t, s.r, "the undo notice", func(st enginetest.State) bool { return noticeWith(st, "/redo") })
	s.do(t, func() error {
		return s.p.Event(s.l, "session.revert.cleared", true, map[string]any{"sessionID": sess})
	})
	waitFor(t, s.r, "the redo", func(st enginetest.State) bool { return !noticeWith(st, "/redo in the terminal restores") })
	s.typedOnly(t)
}
