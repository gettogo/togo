package opencode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// mcpRows runs the mcp source's select over a /api/mcp answer and returns
// each server's state and the actions its row offers.
func mcpRows(t *testing.T, a *spec.Adapter, answer any) map[string]string {
	t.Helper()
	v, err := engine.JQFirst(a.Sources["mcp"].Select, answer, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, el := range v.([]any) {
		acts, err := engine.JQFirst(a.Screens["mcp"].Map["item.actions"], el, nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, n := range acts.([]any) {
			names = append(names, n.(string))
		}
		m := el.(map[string]any)
		out[m["id"].(string)] = m["state"].(string) + ":" + strings.Join(names, ",")
	}
	return out
}

// Seen live on 2.3.0: the MCP list showed a server's state and offered
// nothing to do about it; OpenCode's own list connects, disconnects and
// retries. The server has a route for each (POST
// /api/experimental/mcp/{server}/connect and /disconnect), and a row now
// offers the one that fits its state.
//
// testdata/v2/mcp-servers.json is what a 2.2.5 server listed with three
// servers added (one that starts, one whose program is missing, one added
// disabled); mcp-servers-after.json is its list after the first was
// disconnected and the third connected through those routes (both 204).
func TestMCPRowsOfferConnectAndDisconnect(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	before := mcpRows(t, a, fixture2(t, "mcp-servers.json"))
	for name, want := range map[string]string{
		"fixture_tools": "connected:mcp_disconnect",
		"broken_tools":  "failed:mcp_connect",
		"off_tools":     "disabled:mcp_connect",
	} {
		if before[name] != want {
			t.Errorf("%s: %s, want %s", name, before[name], want)
		}
	}
	after := mcpRows(t, a, fixture2(t, "mcp-servers-after.json"))
	if after["fixture_tools"] != "disabled:mcp_connect" || after["off_tools"] != "connected:mcp_disconnect" {
		t.Errorf("after the switch: %v", after)
	}

	// Not recorded, written from the 2.3.0 schema (Mcp.Status, Mcp.Server):
	// a server still starting has nothing to do yet; one that needs a
	// sign-in is retried like a failed one, unless it is an OAuth
	// integration, whose sign-in runs in OpenCode's terminal.
	var others any
	if err := json.Unmarshal([]byte(`{"data": [
		{"name": "starting", "status": {"status": "pending"}},
		{"name": "locked", "status": {"status": "needs_auth", "error": "401"}},
		{"name": "remote", "status": {"status": "needs_auth", "error": "401"}, "integrationID": "mcp:remote"}]}`), &others); err != nil {
		t.Fatal(err)
	}
	got := mcpRows(t, a, others)
	if got["starting"] != "pending:" || got["locked"] != "needs_auth:mcp_connect" || got["remote"] != "needs_auth:" {
		t.Errorf("other states: %v", got)
	}

	// The routes name the server in the row and the session's folder.
	for name, want := range map[string]string{
		"mcp_connect":    "/api/experimental/mcp/fixture_tools/connect?location%5Bdirectory%5D=%2Fw%2Fmy+project",
		"mcp_disconnect": "/api/experimental/mcp/fixture_tools/disconnect?location%5Bdirectory%5D=%2Fw%2Fmy+project",
	} {
		act, ok := a.Actions[name]
		if !ok || act.HTTP == nil || act.HTTP.Method != "POST" {
			t.Fatalf("action %s: %+v", name, act)
		}
		path, err := engine.Render(act.HTTP.Path, map[string]any{"target": "fixture_tools", "cwd": "/w/my project"})
		if err != nil || path != want {
			t.Errorf("%s posts to %s (%v)", name, path, err)
		}
	}
	// A name cannot leave its place in the path.
	path, _ := engine.Render(a.Actions["mcp_connect"].HTTP.Path, map[string]any{"target": "a/b?c", "cwd": "/w"})
	if !strings.HasPrefix(path, "/api/experimental/mcp/a%2Fb%3Fc/connect?") {
		t.Errorf("odd name renders %s", path)
	}
}
