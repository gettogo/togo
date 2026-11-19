package enginetest

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OpenCode 2.x's routes (opencode2.yaml), answered from the journal the
// sessiongen.OpenCode2 profile writes, as the real server answers from
// its own store. Where a route's real answer was recorded
// (adapters/opencode/testdata/v2), it is served as recorded: models with
// variants, integrations, agents with a custom one, MCP servers before
// and after a connect and a disconnect. Routes with no recorded answer
// (/api/provider, /api/command) are 404, as for a server without them.
// The calls that change something answer 204, as the MCP ones were seen
// to, and write to the stream the event the real server sends.

// v2State is what a 2.x server keeps besides its journal.
type v2State struct {
	mu        sync.Mutex
	fixtures  map[string][]byte
	connected map[string]bool // MCP connect and disconnect calls taken, by "verb name"
}

func newV2State() *v2State {
	dir := filepath.Join(sourceDir(), "harness", "adapters", "opencode", "testdata", "v2")
	fx := map[string][]byte{}
	for _, n := range []string{"models-variants.json", "integrations-methods.json", "agents-custom.json", "mcp-servers.json", "mcp-servers-after.json"} {
		if b, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
			fx[n] = b
		}
	}
	return &v2State{fixtures: fx, connected: map[string]bool{}}
}

// journal2 is what the journal says of the agent's sessions.
type journal2 struct {
	sessions map[string]map[string]any // id -> info, as the session routes give it
	order    []string
	parent   map[string]string
	busy     map[string]int64  // session -> when its running execution started
	step     map[string]string // session -> the running execution's last step
	asks     map[string]string // permission or form id -> its session
	shells   map[string]map[string]any
	shellIDs []string
	msgs     map[string][]map[string]any // session -> its messages, oldest first
}

type event2 struct {
	ID      string         `json:"id"`
	Created int64          `json:"created"`
	Type    string         `json:"type"`
	Data    map[string]any `json:"data"`
}

func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }

func (s *APIServer) journal2() journal2 {
	j := journal2{sessions: map[string]map[string]any{}, parent: map[string]string{}, busy: map[string]int64{},
		step: map[string]string{}, asks: map[string]string{}, shells: map[string]map[string]any{}, msgs: map[string][]map[string]any{}}
	lines, _ := readLines(s.journal, 0)
	h := history2{j: &j}
	for _, l := range lines {
		var ev event2
		if json.Unmarshal(l, &ev) != nil || ev.Data == nil {
			continue
		}
		d := ev.Data
		sess := str(d, "sessionID")
		switch ev.Type {
		case "session.created":
			if _, ok := j.sessions[sess]; !ok {
				j.order = append(j.order, sess)
			}
			info := map[string]any{"id": sess, "title": str(d, "title"), "time": map[string]any{"created": ev.Created, "updated": ev.Created}}
			if p := str(d, "parentID"); p != "" {
				info["parentID"], info["agent"] = p, str(d, "agent")
				j.parent[sess] = p
			}
			j.sessions[sess] = info
		case "session.execution.started":
			j.busy[sess] = ev.Created
		case "session.step.started":
			j.step[sess] = str(d, "assistantMessageID")
		case "session.execution.succeeded", "session.execution.failed", "session.execution.interrupted":
			if info := j.sessions[sess]; info != nil {
				tm := info["time"].(map[string]any)
				tm["updated"] = ev.Created
				if b := j.busy[sess]; b > 0 {
					tm["busy"], tm["idle"] = b, ev.Created
				}
			}
			delete(j.busy, sess)
			delete(j.step, sess)
		case "permission.asked":
			j.asks[str(d, "id")] = sess
		case "form.created":
			if f, ok := d["form"].(map[string]any); ok {
				j.asks[str(f, "id")] = str(f, "sessionID")
			}
		case "shell.created":
			if in, ok := d["info"].(map[string]any); ok {
				j.shells[str(in, "id")] = in
				j.shellIDs = append(j.shellIDs, str(in, "id"))
			}
		case "shell.exited", "shell.deleted":
			delete(j.shells, str(d, "id"))
		}
		h.add(ev)
	}
	return j
}

// route2 answers one request other than the stream.
func (s *APIServer) route2(method string, u *url.URL, body []byte) (int, []byte) {
	path, q := u.Path, u.Query()
	parts := strings.Split(strings.Trim(path, "/"), "/")
	j := s.journal2()
	s.mu.Lock()
	lost := map[string]bool{}
	for k := range s.lost {
		lost[k] = true
	}
	s.mu.Unlock()
	if len(parts) >= 3 && parts[0] == "api" && parts[1] == "session" {
		if _, ok := j.sessions[parts[2]]; !ok || lost[parts[2]] {
			return http.StatusNotFound, nil
		}
	}
	fx := s.v2.fixtures
	switch {
	case method == http.MethodGet && path == "/api/info":
		return http.StatusOK, nil
	case method == http.MethodGet && path == "/api/session":
		return http.StatusOK, s.sessions2(j, q, lost)
	case method == http.MethodGet && len(parts) == 4 && parts[3] == "message":
		return s.messages2(j, parts[2], q)
	case method == http.MethodGet && path == "/api/model" && fx["models-variants.json"] != nil:
		return http.StatusOK, fx["models-variants.json"]
	case method == http.MethodGet && path == "/api/integration" && fx["integrations-methods.json"] != nil:
		return http.StatusOK, fx["integrations-methods.json"]
	case method == http.MethodGet && path == "/api/agent" && fx["agents-custom.json"] != nil:
		return http.StatusOK, fx["agents-custom.json"]
	case method == http.MethodGet && path == "/api/mcp":
		s.v2.mu.Lock()
		after := s.v2.connected["disconnect fixture_tools"] && s.v2.connected["connect off_tools"]
		s.v2.mu.Unlock()
		if after {
			return http.StatusOK, fx["mcp-servers-after.json"]
		}
		return http.StatusOK, fx["mcp-servers.json"]
	case method == http.MethodGet && path == "/api/shell":
		// shells.json: what runs, each with its session in metadata.
		out := []any{}
		for _, id := range j.shellIDs {
			if in, ok := j.shells[id]; ok {
				out = append(out, in)
			}
		}
		return http.StatusOK, jsonOf(map[string]any{"data": out})
	case method == http.MethodPost && path == "/api/session":
		return s.create2(j)
	case method == http.MethodPost && len(parts) == 4 && parts[1] == "session":
		return s.sessionCall2(j, parts[2], parts[3], body)
	case method == http.MethodPost && len(parts) == 6 && parts[1] == "session" && parts[5] == "reply":
		return s.reply2(j, parts[2], parts[3], parts[4], body)
	case method == http.MethodPost && len(parts) == 5 && parts[1] == "integration" && parts[3] == "connect" && parts[4] == "key":
		if !hasField(body, "key") {
			return http.StatusBadRequest, nil
		}
		return http.StatusNoContent, nil
	case method == http.MethodPost && len(parts) == 5 && parts[1] == "experimental" && parts[2] == "mcp" && (parts[4] == "connect" || parts[4] == "disconnect"):
		s.v2.mu.Lock()
		s.v2.connected[parts[4]+" "+parts[3]] = true
		s.v2.mu.Unlock()
		return http.StatusNoContent, nil
	}
	return http.StatusNotFound, nil
}

// sessions2 lists sessions as /api/session does: top-level ones of the
// folder (sessions-archived.json), or a session's children
// (children.json).
func (s *APIServer) sessions2(j journal2, q url.Values, lost map[string]bool) []byte {
	parent := q.Get("parentID")
	out := []any{}
	for i := len(j.order) - 1; i >= 0; i-- {
		id := j.order[i]
		if lost[id] || j.parent[id] != parent {
			continue
		}
		info := j.sessions[id]
		if parent != "" && j.busy[id] > 0 {
			info["time"].(map[string]any)["busy"] = j.busy[id]
		}
		out = append(out, info)
	}
	if parent != "" {
		return jsonOf(map[string]any{"data": out, "cursor": map[string]any{}})
	}
	return jsonOf(map[string]any{"data": out})
}

// create2 makes a new top-level session, as POST /api/session does, and
// says so on the stream.
func (s *APIServer) create2(j journal2) (int, []byte) {
	now := time.Now().UTC()
	id := "ses_" + strconv.FormatInt(now.UnixNano(), 16) + "api"
	title := "New session - " + now.Format("2006-01-02T15:04:05.000Z")
	s.Append(map[string]any{"id": "evt_" + id, "created": now.UnixMilli(), "type": "session.created",
		"data": map[string]any{"sessionID": id, "title": title}})
	return http.StatusOK, jsonOf(map[string]any{"id": id, "title": title, "time": map[string]any{"created": now.UnixMilli(), "updated": now.UnixMilli()}})
}

// sessionCall2 takes the calls on one session that are not replies.
func (s *APIServer) sessionCall2(j journal2, sess, call string, body []byte) (int, []byte) {
	now := time.Now().UTC().UnixMilli()
	evID := "evt_" + strconv.FormatInt(time.Now().UnixNano(), 16)
	switch call {
	case "model":
		var req struct {
			Model map[string]any `json:"model"`
		}
		if json.Unmarshal(body, &req) != nil || str(req.Model, "id") == "" {
			return http.StatusBadRequest, nil
		}
		// variant-selected.sse: the model with its variant.
		s.Append(map[string]any{"id": evID, "created": now, "type": "session.model.selected",
			"data": map[string]any{"sessionID": sess, "model": req.Model}})
	case "agent":
		var req struct {
			Agent string `json:"agent"`
		}
		if json.Unmarshal(body, &req) != nil || req.Agent == "" {
			return http.StatusBadRequest, nil
		}
		s.Append(map[string]any{"id": evID, "created": now, "type": "session.agent.selected",
			"data": map[string]any{"sessionID": sess, "agent": req.Agent}})
	case "command":
		if !hasField(body, "name") {
			return http.StatusBadRequest, nil
		}
	case "compact":
	case "interrupt":
		// events.sse: the running step fails as aborted, the execution is
		// interrupted. An idle session has nothing to stop.
		if j.busy[sess] > 0 {
			s.Append(map[string]any{"id": evID + "a", "created": now, "type": "session.step.failed",
				"data": map[string]any{"sessionID": sess, "assistantMessageID": j.step[sess], "error": map[string]any{"type": "aborted", "message": "Step interrupted"}}},
				map[string]any{"id": evID + "b", "created": now, "type": "session.execution.interrupted", "data": map[string]any{"sessionID": sess}})
		}
		if on := s.OnAbort; on != nil {
			go on()
		}
	default:
		return http.StatusNotFound, nil
	}
	return http.StatusNoContent, nil
}

// reply2 answers a permission or a form and says so on the stream
// (events.sse permission.replied; forms.sse form.replied).
func (s *APIServer) reply2(j journal2, sess, kind, id string, body []byte) (int, []byte) {
	if j.asks[id] != sess {
		return http.StatusNotFound, nil
	}
	now := time.Now().UTC().UnixMilli()
	switch kind {
	case "permission":
		var req struct {
			Decision string `json:"decision"`
		}
		if json.Unmarshal(body, &req) != nil || req.Decision == "" {
			return http.StatusBadRequest, nil
		}
		s.Append(map[string]any{"id": "evt_reply_" + id, "created": now, "type": "permission.replied",
			"data": map[string]any{"sessionID": sess, "requestID": id, "reply": req.Decision}})
	case "form":
		var req struct {
			Answer map[string]any `json:"answer"`
		}
		if json.Unmarshal(body, &req) != nil || req.Answer == nil {
			return http.StatusBadRequest, nil
		}
		s.Append(map[string]any{"id": "evt_reply_" + id, "created": now, "type": "form.replied",
			"data": map[string]any{"id": id, "sessionID": sess, "answer": req.Answer}})
	default:
		return http.StatusNotFound, nil
	}
	return http.StatusNoContent, nil
}
