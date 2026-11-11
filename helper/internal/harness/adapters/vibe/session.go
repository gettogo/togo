package vibe

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
)

// session maps Vibe 3.1's recovery journal. Of its records only
// projection_delta matters: the public history Vibe's own clients see, as
// ops on entries (append_entry, replace_entry, set_history_entries) and a
// set_envelope whose latestTurn says how the turn stands (in_progress,
// completed, failed with an error, interrupted). Streamed text is never
// journaled, only settled entries, so a reply shows once it is complete.
type session struct {
	env harness.Env

	mu     sync.Mutex
	state  map[string]string
	loaded bool
	turns  map[string]*turn
	order  []string                // turn ids, oldest first
	tools  map[string]harness.Item // running effects by entry id
	failed map[string]bool         // turns whose error notice was shown
	asks   map[string]harness.Item // open prompts (ask.go) by item id
	gates  map[string]string       // approval callback id -> the effect it gates
}

type turn struct {
	started time.Time
	last    time.Time
	steps   map[string]bool
	state   string
	ended   *time.Time
}

func newSession(env harness.Env) *session {
	return &session{env: env, state: map[string]string{}, turns: map[string]*turn{},
		tools: map[string]harness.Item{}, failed: map[string]bool{},
		asks: map[string]harness.Item{}, gates: map[string]string{}}
}

func (s *session) Name() string { return MapperSession }

func (s *session) State() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.state))
	for k, v := range s.state {
		out[k] = v
	}
	return out
}

type record struct {
	Type    string `json:"type"`
	Payload struct {
		Delta []op `json:"delta"`
		// callback_registered and callback_resolved (ask.go).
		CallbackID string          `json:"callback_id"`
		Routing    *callbackEntry  `json:"routing"`
		Result     *callbackResult `json:"result"`
	} `json:"payload"`
}

type op struct {
	Op      string    `json:"op"`
	Entry   *entry    `json:"entry"`
	Entries []entry   `json:"entries"`
	State   *envelope `json:"state"`
}

type entry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	TurnID    string          `json:"turnId"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Text      string          `json:"text"`
	Message   string          `json:"message"`
	Level     string          `json:"level"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Outcome   *struct {
		Type  string `json:"type"`
		Cause string `json:"cause"`
	} `json:"outcome"`
	Detail struct {
		ToolName string          `json:"toolName"`
		Input    json.RawMessage `json:"input"`
	} `json:"detail"`
	Details struct {
		Model string          `json:"model"`
		Agent string          `json:"agent"`
		Error json.RawMessage `json:"error"`
	} `json:"details"`
	State struct {
		Status     string          `json:"status"`
		OutputText string          `json:"outputText"`
		Output     json.RawMessage `json:"output"`
		Reason     string          `json:"reason"`
		Error      *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"state"`
}

type envelope struct {
	LatestTurn *struct {
		ID          string     `json:"id"`
		Status      string     `json:"status"`
		StartedAt   int64      `json:"startedAt"`
		CompletedAt int64      `json:"completedAt"`
		Reason      string     `json:"reason"`
		StopReason  string     `json:"stopReason"`
		Error       *turnError `json:"error"`
	} `json:"latestTurn"`
	Session struct {
		ID string `json:"id"`
	} `json:"session"`
}

// turnError is a failed turn's PublicError.
type turnError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details struct {
		HTTPStatus    int    `json:"httpStatus"`
		Reason        string `json:"reason"`
		ExceptionType string `json:"exceptionType"`
	} `json:"details"`
}

func (s *session) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var r record
	if json.Unmarshal(rec.Data, &r) != nil {
		return nil, nil
	}
	switch {
	case r.Type == "callback_registered" && r.Payload.Routing != nil:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.callbackOpened(*r.Payload.Routing), nil
	case r.Type == "callback_resolved" && r.Payload.Result != nil:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.callbackResolved(r.Payload.CallbackID, *r.Payload.Result), nil
	case r.Type != "projection_delta" || len(r.Payload.Delta) == 0:
		return nil, nil
	}
	hookstyle.MarkLive(s.env.SessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []harness.Item
	if !s.loaded {
		s.loaded = true
		if id := sessionOf(r.Payload.Delta); id != "" {
			items = s.history(id)
		}
	}
	for _, o := range r.Payload.Delta {
		switch o.Op {
		case "append_entry", "replace_entry":
			if o.Entry != nil {
				items = append(items, s.entry(*o.Entry)...)
			}
		case "set_history_entries":
			for _, e := range o.Entries {
				items = append(items, s.entry(e)...)
			}
		case "set_envelope":
			if o.State != nil {
				items = append(items, s.envelope(*o.State)...)
			}
		}
	}
	return items, nil
}

func sessionOf(delta []op) string {
	for _, o := range delta {
		if o.Entry != nil && o.Entry.SessionID != "" {
			return o.Entry.SessionID
		}
		if o.State != nil && o.State.Session.ID != "" {
			return o.State.Session.ID
		}
	}
	return ""
}

func ms(v int64) time.Time {
	if v <= 0 {
		return time.Now().UTC()
	}
	return time.UnixMilli(v).UTC()
}

// open finds or starts the turn an entry belongs to. A new turn closes an
// earlier one still open, which only history replay leaves so.
func (s *session) open(id string, at time.Time) (*turn, []harness.Item) {
	if id == "" {
		return nil, nil
	}
	if t := s.turns[id]; t != nil {
		if at.After(t.last) {
			t.last = at
		}
		return t, nil
	}
	var items []harness.Item
	for _, prev := range s.order {
		if p := s.turns[prev]; p.ended == nil {
			items = append(items, s.end(prev, harness.StateOK, p.last)...)
		}
	}
	t := &turn{started: at, last: at, steps: map[string]bool{}, state: harness.StateRunning}
	s.turns[id] = t
	s.order = append(s.order, id)
	if len(s.order) > 200 {
		delete(s.turns, s.order[0])
		s.order = s.order[1:]
	}
	s.working(true, at)
	return t, append(items, s.turnItem(id))
}

func (s *session) entry(e entry) []harness.Item {
	at := ms(e.CreatedAt)
	t, items := s.open(e.TurnID, at)
	switch e.Type {
	case "message":
		body := text(e.Content)
		if body == "" {
			return items
		}
		switch e.Role {
		case "user":
			return append(items, harness.Item{ID: e.ID, TurnID: e.TurnID, Kind: harness.KindUser, At: at,
				User: &harness.UserBody{Text: hookstyle.Clip(body), Source: "unknown"}})
		case "assistant":
			if e.Outcome != nil && e.Outcome.Type == "discarded" && e.Outcome.Cause == "retry" {
				return items
			}
			return append(items, harness.Item{ID: e.ID, TurnID: e.TurnID, Kind: harness.KindAssistant, At: at,
				Assistant: &harness.TextBody{Text: hookstyle.Clip(body)}})
		}
	case "reasoning":
		if strings.TrimSpace(e.Text) == "" {
			return items
		}
		return append(items, harness.Item{ID: e.ID, TurnID: e.TurnID, Kind: harness.KindThinking, At: at,
			Thinking: &harness.TextBody{Text: hookstyle.Clip(e.Text)}})
	case "effect":
		if isAsk(e.Detail.ToolName) {
			if x, ok := s.askEffect(e, at); ok {
				return append(items, x)
			}
		}
		x := s.effect(e, at)
		if t != nil && !t.steps[e.ID] {
			t.steps[e.ID] = true
			items = append(items, s.turnItem(e.TurnID))
		}
		return append(items, x)
	case "notice":
		lvl := e.Level
		if lvl != "warning" && lvl != "error" {
			lvl = "info"
		}
		if e.Message == "" {
			return items
		}
		return append(items, harness.Item{ID: e.ID, TurnID: e.TurnID, Kind: harness.KindNotice, At: at,
			Notice: &harness.NoticeBody{Level: lvl, Text: hookstyle.Truncate(e.Message, harness.MaxNoticeText)}})
	case "checkpoint":
		return append(items, s.checkpoint(e, at)...)
	}
	return items
}

func (s *session) checkpoint(e entry, at time.Time) []harness.Item {
	n := harness.Item{ID: e.ID, TurnID: e.TurnID, Kind: harness.KindNotice, At: at}
	switch e.Kind {
	case "compaction":
		if e.Message != "Context compacted" {
			return nil // started, or failed: the turn's error says why
		}
		n.Notice = &harness.NoticeBody{Level: "info", Code: harness.NoticeCompacted, Text: "Conversation compacted"}
	case "model_change":
		if e.Details.Model != "" {
			s.state["model"] = e.Details.Model
		}
		n.Notice = &harness.NoticeBody{Level: "info", Code: harness.NoticeModel, Text: e.Message}
	case "agent_change":
		n.Notice = &harness.NoticeBody{Level: "info", Code: harness.NoticeMode, Text: e.Message}
	default:
		return nil
	}
	return []harness.Item{n}
}

func (s *session) effect(e entry, at time.Time) harness.Item {
	name := e.Detail.ToolName
	if name == "" {
		name = e.Title
	}
	short := name
	if i := strings.LastIndexByte(short, '.'); i >= 0 {
		short = short[i+1:]
	}
	title, cat, subj := hookstyle.ToolTitle(short, e.Detail.Input)
	b := &harness.ToolBody{CallID: e.ID, Name: short, Title: hookstyle.Truncate(title, harness.MaxTitle),
		Category: cat, Subject: subj, State: harness.StateRunning, Started: at}
	if prev, ok := s.tools[e.ID]; ok {
		b.Started = prev.Tool.Started
	}
	x := harness.Item{ID: e.ID, TurnID: e.TurnID, Kind: harness.KindTool, At: b.Started, Tool: b}
	st := e.State
	switch st.Status {
	case "", "running", "open":
		s.tools[e.ID] = x
		return x
	case "completed":
		b.State = harness.StateOK
	case "cancelled", "skipped":
		b.State = harness.StateCancelled
	default:
		b.State = harness.StateFailed
		if st.Error != nil && st.Error.Code == "tool_denied" {
			b.State = harness.StateDenied
		}
	}
	end := ms(e.UpdatedAt)
	b.Ended = &end
	b.Output, b.ExitCode = effectOutput(st.OutputText, st.Output)
	if b.Output == "" {
		switch {
		case st.Error != nil && st.Error.Message != "":
			b.Output = hookstyle.Tail(st.Error.Message, harness.MaxToolOutput)
		case st.Reason != "":
			b.Output = hookstyle.Tail(st.Reason, harness.MaxToolOutput)
		}
	}
	delete(s.tools, e.ID)
	return x
}

// effectOutput is a settled effect's output: its text, else the tool's
// structured result (stdout for a shell call) or its text content.
func effectOutput(textOut string, raw json.RawMessage) (string, *int) {
	var o struct {
		Structured json.RawMessage `json:"structured_content"`
		Content    json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(raw, &o)
	var code *int
	var sh struct {
		ReturnCode *int   `json:"returncode"`
		Stdout     string `json:"stdout"`
		Stderr     string `json:"stderr"`
	}
	if json.Unmarshal(o.Structured, &sh) == nil && sh.ReturnCode != nil {
		code = sh.ReturnCode
		if textOut == "" {
			textOut = strings.TrimRight(sh.Stdout+sh.Stderr, "\n")
			return hookstyle.Tail(textOut, harness.MaxToolOutput), code
		}
	}
	if textOut != "" {
		return hookstyle.Tail(textOut, harness.MaxToolOutput), code
	}
	if t := text(o.Content); t != "" {
		return hookstyle.Tail(t, harness.MaxToolOutput), code
	}
	if len(o.Structured) > 0 && string(o.Structured) != "null" {
		out, _ := hookstyle.ToolOutput(o.Structured)
		return out, code
	}
	return "", code
}

func (s *session) envelope(v envelope) []harness.Item {
	if v.Session.ID != "" {
		s.state["session_id"] = v.Session.ID
	}
	lt := v.LatestTurn
	if lt == nil || lt.ID == "" {
		return nil
	}
	t, items := s.open(lt.ID, ms(lt.StartedAt))
	if lt.StartedAt > 0 {
		t.started = ms(lt.StartedAt)
	}
	switch lt.Status {
	case "in_progress":
		if t.ended != nil {
			t.ended, t.state = nil, harness.StateRunning
			s.working(true, time.Now().UTC())
		}
		return append(items, s.turnItem(lt.ID))
	case "completed":
		return append(items, s.end(lt.ID, harness.StateOK, ms(lt.CompletedAt))...)
	case "interrupted":
		return append(items, s.end(lt.ID, harness.StateInterrupted, ms(lt.CompletedAt))...)
	case "failed":
		at := ms(lt.CompletedAt)
		items = append(items, s.end(lt.ID, harness.StateFailed, at)...)
		if !s.failed[lt.ID] {
			s.failed[lt.ID] = true
			items = append(items, harness.Item{ID: lt.ID + ":error", TurnID: lt.ID, Kind: harness.KindNotice, At: at,
				Notice: failNotice(lt.Error)})
		}
		return items
	}
	return items
}

// end settles a turn, and any of its effects still running.
func (s *session) end(id, state string, at time.Time) []harness.Item {
	t := s.turns[id]
	if t == nil {
		return nil
	}
	var items []harness.Item
	for eid, x := range s.tools {
		if x.TurnID != id {
			continue
		}
		b := *x.Tool
		b.State, b.Ended = harness.StateInterrupted, &at
		x.Tool = &b
		items = append(items, x)
		delete(s.tools, eid)
	}
	items = append(items, s.closeAsks(id)...)
	t.state, t.ended = state, &at
	s.working(false, at)
	return append(items, s.turnItem(id))
}

func (s *session) working(on bool, at time.Time) {
	s.state["working"], s.state["idle"] = strconv.FormatBool(on), strconv.FormatBool(!on)
	s.state[harness.StateWorkingAt] = harness.StampWorking(at)
}

func (s *session) turnItem(id string) harness.Item {
	t := s.turns[id]
	return harness.Item{ID: id, TurnID: id, Kind: harness.KindTurn, At: t.started,
		Turn: &harness.TurnBody{State: t.state, Started: t.started, Ended: t.ended, Steps: len(t.steps)}}
}

// text reads content given as a string or as a list of {type, text} parts.
func text(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		if p.Text != "" {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, "\n")
}
