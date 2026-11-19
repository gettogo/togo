package enginetest

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// apiState is what the journal says of the agent's sessions: the routes
// answer from it, as the real server answers from its own store.
type apiState struct {
	sessions map[string]json.RawMessage // id -> newest info
	order    []string
	parent   map[string]string
	todos    map[string]json.RawMessage
	diffs    map[string]json.RawMessage
	asks     map[string]string // request id -> session id
	main     string            // the newest top-level session prompted or selected
}

func (s *APIServer) state() apiState {
	st := apiState{sessions: map[string]json.RawMessage{}, parent: map[string]string{}, todos: map[string]json.RawMessage{},
		diffs: map[string]json.RawMessage{}, asks: map[string]string{}}
	lines, _ := readLines(s.journal, 0)
	for _, l := range lines {
		typ, props, err := sessiongen.OpenCodeEvent(l)
		if err != nil {
			continue
		}
		var p struct {
			SessionID string          `json:"sessionID"`
			ID        string          `json:"id"`
			Info      json.RawMessage `json:"info"`
			Todos     json.RawMessage `json:"todos"`
			Diff      json.RawMessage `json:"diff"`
		}
		if json.Unmarshal(props, &p) != nil {
			continue
		}
		switch typ {
		case "session.created", "session.updated":
			var in struct {
				ID       string `json:"id"`
				ParentID string `json:"parentID"`
			}
			if json.Unmarshal(p.Info, &in) != nil || in.ID == "" {
				continue
			}
			if _, ok := st.sessions[in.ID]; !ok {
				st.order = append(st.order, in.ID)
			}
			st.sessions[in.ID] = p.Info
			if in.ParentID != "" {
				st.parent[in.ID] = in.ParentID
			} else if typ == "session.created" {
				st.main = in.ID
			}
		case "tui.session.select":
			st.main = p.SessionID
		case "message.updated":
			var in struct {
				Role      string `json:"role"`
				SessionID string `json:"sessionID"`
			}
			if json.Unmarshal(p.Info, &in) == nil && in.Role == "user" && st.parent[in.SessionID] == "" {
				st.main = in.SessionID
			}
		case "todo.updated":
			st.todos[p.SessionID] = p.Todos
		case "session.diff":
			st.diffs[p.SessionID] = p.Diff
		case "permission.asked", "question.asked":
			st.asks[p.ID] = p.SessionID
		}
	}
	return st
}

func jsonOf(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// route answers one request other than the stream.
func (s *APIServer) route(method, path string, body []byte) (int, []byte) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	st := s.state()
	s.mu.Lock()
	lost := map[string]bool{}
	for k := range s.lost {
		lost[k] = true
	}
	s.mu.Unlock()
	if len(parts) >= 2 && (parts[0] == "session" || parts[0] == "api" && len(parts) >= 3) {
		id := parts[1]
		if parts[0] == "api" {
			id = parts[2]
		}
		if _, ok := st.sessions[id]; !ok || lost[id] {
			return http.StatusNotFound, nil
		}
	}
	switch {
	case method == http.MethodGet && path == "/path":
		return http.StatusOK, nil
	case method == http.MethodGet && path == "/config/providers" && len(s.providers) > 0:
		return http.StatusOK, s.providers
	case method == http.MethodGet && path == "/session":
		out := []json.RawMessage{}
		for _, id := range st.order {
			if !lost[id] {
				out = append(out, st.sessions[id])
			}
		}
		return http.StatusOK, jsonOf(out)
	case method == http.MethodGet && len(parts) == 3 && parts[0] == "session":
		switch parts[2] {
		case "todo":
			return http.StatusOK, orEmpty(st.todos[parts[1]])
		case "diff":
			return http.StatusOK, orEmpty(st.diffs[parts[1]])
		case "children":
			out := []json.RawMessage{}
			for _, id := range st.order {
				if st.parent[id] == parts[1] {
					out = append(out, st.sessions[id])
				}
			}
			return http.StatusOK, jsonOf(out)
		}
	case method == http.MethodPost && len(parts) == 3 && parts[0] == "session" && parts[2] == "prompt_async":
		var req struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		}
		if json.Unmarshal(body, &req) != nil || len(req.Parts) == 0 || strings.TrimSpace(req.Parts[0].Text) == "" {
			return http.StatusBadRequest, nil
		}
		if on := s.OnPrompt; on != nil {
			go on(req.Parts[0].Text)
		}
		return http.StatusNoContent, nil
	case method == http.MethodPost && len(parts) == 3 && parts[0] == "session" && parts[2] == "abort":
		if on := s.OnAbort; on != nil {
			go on()
		}
		return http.StatusOK, []byte("true")
	case method == http.MethodPost && len(parts) == 3 && (parts[0] == "permission" || parts[0] == "question") && parts[2] == "reply":
		return s.reply(st, parts[0], parts[1], body)
	case method == http.MethodPost && path == "/session":
		return s.create(st)
	case method == http.MethodPut && len(parts) == 2 && parts[0] == "auth":
		var req struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}
		if json.Unmarshal(body, &req) != nil || req.Type != "api" || req.Key == "" {
			return http.StatusBadRequest, nil
		}
		return http.StatusOK, []byte("true")
	case method == http.MethodPost && path == "/tui/select-session":
		var req struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(body, &req) != nil || st.sessions[req.SessionID] == nil {
			return http.StatusNotFound, nil
		}
		// The TUI says which session it shows now (needs_test.go:19).
		s.Append(map[string]any{"type": "tui.session.select", "properties": map[string]any{"sessionID": req.SessionID}})
		return http.StatusOK, []byte("true")
	case method == http.MethodPost && len(parts) == 4 && parts[0] == "api" && parts[1] == "session" && (parts[3] == "model" || parts[3] == "agent"),
		method == http.MethodPost && len(parts) == 3 && parts[0] == "session" && (parts[2] == "command" || parts[2] == "summarize"):
		// Taken when the body names what it asks for; a request without
		// it is refused, as a server that validates its input does.
		if !hasField(body, map[string]string{"model": "model", "agent": "agent", "command": "command", "summarize": "modelID"}[parts[len(parts)-1]]) {
			return http.StatusBadRequest, nil
		}
		return http.StatusOK, []byte("true")
	}
	return http.StatusNotFound, nil
}

// create makes a new top-level session, as POST /session does: its info
// is the newest session's with a new id, title and time, and the stream
// says it was created (session.created, events.sse).
func (s *APIServer) create(st apiState) (int, []byte) {
	var base map[string]any
	for i := len(st.order) - 1; i >= 0 && base == nil; i-- {
		if st.parent[st.order[i]] == "" {
			_ = json.Unmarshal(st.sessions[st.order[i]], &base)
		}
	}
	if base == nil {
		return http.StatusInternalServerError, nil
	}
	now := time.Now().UTC()
	id := "ses_" + strconv.FormatInt(now.UnixNano(), 16) + "api"
	info := map[string]any{}
	for _, k := range []string{"slug", "version", "projectID", "directory", "path", "cost", "tokens"} {
		if v, ok := base[k]; ok {
			info[k] = v
		}
	}
	info["id"], info["title"] = id, "New session - "+now.Format("2006-01-02T15:04:05.000Z")
	info["time"] = map[string]any{"created": now.UnixMilli(), "updated": now.UnixMilli()}
	s.Append(map[string]any{"id": "evt_" + id, "type": "session.created", "properties": map[string]any{"sessionID": id, "info": info}})
	return http.StatusOK, jsonOf(info)
}

// hasField reports whether a JSON object body has a non-empty field.
func hasField(body []byte, name string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return false
	}
	v := strings.TrimSpace(string(m[name]))
	return v != "" && v != "null" && v != `""` && v != "{}"
}

func orEmpty(b json.RawMessage) []byte {
	if len(b) == 0 {
		return []byte("[]")
	}
	return b
}

// reply answers a permission or question and says so on the stream with
// the event the real agent sends (events.sse: permission.replied,
// question.replied).
func (s *APIServer) reply(st apiState, kind, id string, body []byte) (int, []byte) {
	sess, ok := st.asks[id]
	if !ok {
		return http.StatusNotFound, nil
	}
	if kind == "permission" {
		var req struct {
			Reply string `json:"reply"`
		}
		if json.Unmarshal(body, &req) != nil || req.Reply == "" {
			return http.StatusBadRequest, nil
		}
		s.Append(map[string]any{"id": "evt_reply_" + id, "type": "permission.replied",
			"properties": map[string]any{"sessionID": sess, "requestID": id, "reply": req.Reply}})
		return http.StatusOK, []byte("true")
	}
	var req struct {
		Answers [][]string `json:"answers"`
	}
	if json.Unmarshal(body, &req) != nil {
		return http.StatusBadRequest, nil
	}
	s.Append(map[string]any{"id": "evt_reply_" + id, "type": "question.replied",
		"properties": map[string]any{"sessionID": sess, "requestID": id, "answers": req.Answers}})
	return http.StatusOK, []byte("true")
}

// WaitCall waits until a call matching ok has been made, and returns it.
func (s *APIServer) WaitCall(limit time.Duration, ok func(APICall) bool) (APICall, bool) {
	for deadline := time.Now().Add(limit); ; time.Sleep(10 * time.Millisecond) {
		for _, c := range s.Calls() {
			if ok(c) {
				return c, true
			}
		}
		if time.Now().After(deadline) {
			return APICall{}, false
		}
	}
}

// Journal is the path of the journal the server streams.
func (s *APIServer) Journal() string { return s.journal }
