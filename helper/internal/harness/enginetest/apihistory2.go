package enginetest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The messages route of OpenCode 2.x, GET /api/session/{id}/message:
// a conversation's messages, built from the journal in the shape
// testdata/v2/messages.json records (2.2.5): a user prompt {id, time,
// text, files, agents, type}, an assistant message per step {id, time,
// type, agent, model, content} whose content holds reasoning, tool and
// text parts, and an idle marker after a turn that succeeded. A page is
// `limit` messages, newest first; `cursor.next` names the oldest one, and
// a request with that cursor (and no order) gives the page before it. As
// recorded, a page that holds messages always has a next cursor; the page
// past the start is empty, with an empty cursor (children.json).

// history2 builds the messages of every session from the events.
type history2 struct {
	j     *journal2
	tools map[string]map[string]any // call id -> its part
}

func (h *history2) last(sess, id string) map[string]any {
	ms := h.j.msgs[sess]
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i]["id"] == id {
			return ms[i]
		}
	}
	return nil
}

func (h *history2) add(ev event2) {
	if h.tools == nil {
		h.tools = map[string]map[string]any{}
	}
	d := ev.Data
	sess, msg := str(d, "sessionID"), str(d, "assistantMessageID")
	push := func(m map[string]any) { h.j.msgs[sess] = append(h.j.msgs[sess], m) }
	content := func(part map[string]any) {
		if m := h.last(sess, msg); m != nil {
			m["content"] = append(m["content"].([]any), part)
		}
	}
	switch ev.Type {
	case "session.inbox.enqueued":
		item, _ := d["item"].(map[string]any)
		payload, _ := item["payload"].(map[string]any)
		if str(item, "type") == "user" {
			push(map[string]any{"id": str(d, "inboxID"), "time": map[string]any{"created": ev.Created}, "text": str(payload, "text"),
				"files": []any{}, "agents": []any{}, "type": "user"})
		}
	case "session.step.started":
		model, _ := d["model"].(map[string]any)
		ref := map[string]any{"id": str(model, "id"), "providerID": str(model, "providerID"), "variant": "default"}
		if v := str(model, "variant"); v != "" {
			ref["variant"] = v
		}
		push(map[string]any{"id": msg, "time": map[string]any{"created": ev.Created, "completed": ev.Created}, "type": "assistant",
			"agent": str(d, "agent"), "model": ref, "content": []any{}})
	case "session.reasoning.ended":
		content(map[string]any{"type": "reasoning", "text": str(d, "text")})
	case "session.text.ended":
		content(map[string]any{"type": "text", "text": str(d, "text")})
	case "session.tool.input.started":
		part := map[string]any{"type": "tool", "id": str(d, "id"), "name": str(d, "name"),
			"state": map[string]any{"status": "running"}, "time": map[string]any{"created": ev.Created}}
		h.tools[str(d, "id")] = part
		content(part)
	case "session.tool.called":
		if part := h.tools[str(d, "id")]; part != nil {
			part["state"].(map[string]any)["input"] = d["input"]
		}
	case "session.tool.success":
		if part := h.tools[str(d, "id")]; part != nil {
			st := part["state"].(map[string]any)
			st["status"], st["content"], st["metadata"] = "completed", d["content"], d["metadata"]
			part["time"].(map[string]any)["completed"] = ev.Created
			delete(h.tools, str(d, "id"))
		}
	case "session.execution.succeeded":
		push(map[string]any{"id": "msg_" + strings.TrimPrefix(ev.ID, "evt_"), "time": map[string]any{"created": ev.Created}, "type": "idle", "outcome": "succeeded"})
	}
	if m := h.last(sess, msg); m != nil && ev.Type != "session.step.started" {
		m["time"].(map[string]any)["completed"] = ev.Created
	}
}

// cursor2 is the page cursor: the id it pages from, as the real one
// encodes it (messages.json cursor.next).
type cursor2 struct {
	ID        string `json:"id"`
	Order     string `json:"order"`
	Direction string `json:"direction"`
}

func (s *APIServer) messages2(j journal2, sess string, q url.Values) (int, []byte) {
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 || limit > 200 {
		return http.StatusBadRequest, nil
	}
	ms := j.msgs[sess]
	end := len(ms) // messages before end are on this page or older
	if c := q.Get("cursor"); c != "" {
		if q.Get("order") != "" {
			return http.StatusBadRequest, nil // protocol/src/groups/message.ts, opencode2.yaml:51
		}
		raw, err := base64.RawURLEncoding.DecodeString(c)
		var cur cursor2
		if err != nil || json.Unmarshal(raw, &cur) != nil || cur.Direction != "next" {
			return http.StatusBadRequest, nil
		}
		end = -1
		for i, m := range ms {
			if m["id"] == cur.ID {
				end = i
			}
		}
		if end < 0 {
			return http.StatusBadRequest, nil
		}
	} else if q.Get("order") != "desc" {
		return http.StatusBadRequest, nil
	}
	start := max(end-limit, 0)
	page := make([]any, 0, end-start)
	for i := end - 1; i >= start; i-- {
		page = append(page, ms[i])
	}
	cursor := map[string]any{}
	if len(page) > 0 {
		b, _ := json.Marshal(cursor2{ID: str(ms[start], "id"), Order: "desc", Direction: "next"})
		cursor["next"] = base64.RawURLEncoding.EncodeToString(b)
	}
	return http.StatusOK, jsonOf(map[string]any{"data": page, "cursor": cursor})
}
