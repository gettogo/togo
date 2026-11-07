package gemini

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// chat maps Gemini's chat file (~/.gemini/tmp/<project>/chats/session-*.json,
// {sessionId, startTime, lastUpdated, messages[]}), which Gemini rewrites
// whole after each change. Every copy is mapped in full and only items
// that differ from what was sent before are emitted.
type chat struct {
	env     harness.Env
	created time.Time

	mu      sync.Mutex
	session string
	started time.Time
	sent    map[string]uint64 // item id -> hash of its JSON as last sent
	status  string
	state   map[string]string
}

func newChat(env harness.Env) *chat {
	c := &chat{env: env, sent: map[string]uint64{}, state: map[string]string{}}
	c.created, _ = time.Parse(time.RFC3339, env.Vars["created"])
	return c
}

func (c *chat) Name() string { return MapperChat }

func (c *chat) State() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.state))
	for k, v := range c.state {
		out[k] = v
	}
	return out
}

type conversation struct {
	SessionID   string           `json:"sessionId"`
	StartTime   string           `json:"startTime"`
	LastUpdated string           `json:"lastUpdated"`
	Messages    []map[string]any `json:"messages"`
}

func (c *chat) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var doc conversation
	if json.Unmarshal(rec.Data, &doc) != nil || doc.SessionID == "" {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.accept(doc, rec.At) {
		return nil, nil
	}
	items, turnOpen, status := c.build(doc, rec.At)
	c.state["harness_session"] = doc.SessionID
	c.state["working"] = fmt.Sprint(turnOpen)
	var out []harness.Item
	for _, it := range items {
		b, _ := json.Marshal(it)
		h := fnv.New64a()
		h.Write(b)
		if sum, ok := c.sent[it.ID]; ok && sum == h.Sum64() {
			continue
		}
		c.sent[it.ID] = h.Sum64()
		out = append(out, it)
	}
	var ops []harness.Op
	if b, _ := json.Marshal(status); status != nil && string(b) != c.status {
		c.status = string(b)
		ops = append(ops, harness.Op{Op: harness.OpStatus, Status: status})
	}
	return out, ops
}

// accept decides whether a chat file is this session's: the first one
// written since the session started, then only that one, unless a newer
// conversation starts later (a /clear or a resume in the same pane).
func (c *chat) accept(doc conversation, now time.Time) bool {
	updated := stamp(doc.LastUpdated, now)
	started := stamp(doc.StartTime, now)
	switch {
	case doc.SessionID == c.session:
		return true
	case !c.created.IsZero() && updated.Before(c.created.Add(-time.Minute)):
		return false
	case c.session == "" || started.After(c.started):
		c.session, c.started = doc.SessionID, started
		c.sent = map[string]uint64{}
		return true
	}
	return false
}

// build maps the whole conversation; turnOpen is true while the last turn
// has no final reply.
func (c *chat) build(doc conversation, now time.Time) (items []harness.Item, turnOpen bool, status *harness.StatusBlock) {
	var turn *harness.Item
	files := map[string]bool{}
	endTurn := func(state string, at time.Time) {
		if turn != nil && turn.Turn.Ended == nil {
			turn.Turn.State, turn.Turn.Ended = state, ptr(at)
		}
	}
	for _, m := range doc.Messages {
		id := str(m, "id")
		if id == "" {
			continue
		}
		at := stamp(str(m, "timestamp"), now)
		tid := ""
		if turn != nil {
			tid = turn.ID
		}
		switch str(m, "type") {
		case "user":
			text := parts(m["displayContent"])
			if text == "" {
				text = parts(m["content"])
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			if turn != nil {
				endTurn(harness.StateOK, at)
				items = append(items, *turn)
			}
			turn = &harness.Item{ID: "turn-" + id, TurnID: "turn-" + id, Kind: harness.KindTurn, At: at,
				Turn: &harness.TurnBody{State: harness.StateRunning, Started: at}}
			files = map[string]bool{}
			items = append(items, harness.Item{ID: id, TurnID: turn.ID, Kind: harness.KindUser, At: at,
				User: &harness.UserBody{Text: clip(text), Source: "unknown"}})
		case "gemini":
			if model := str(m, "model"); model != "" {
				c.state["model"] = model
			}
			if th := thoughts(m["thoughts"]); th != "" {
				items = append(items, harness.Item{ID: id + "-thinking", TurnID: tid, Kind: harness.KindThinking, At: at,
					Thinking: &harness.TextBody{Text: clip(th)}})
			}
			pending := false
			for _, tc := range list(m["toolCalls"]) {
				it, plan := toolItem(obj(tc), tid, at, c.env.Cwd)
				if it == nil {
					continue
				}
				if it.Tool.State == harness.StateRunning || it.Tool.State == harness.StateWaiting {
					pending = true
				}
				if turn != nil {
					turn.Turn.Steps++
					for _, f := range it.Tool.Files {
						if !files[f.Path] {
							files[f.Path] = true
							turn.Turn.FilesChanged++
						}
					}
				}
				items = append(items, *it)
				if plan != nil {
					items = append(items, *plan)
				}
			}
			if text := parts(m["content"]); strings.TrimSpace(text) != "" {
				items = append(items, harness.Item{ID: id, TurnID: tid, Kind: harness.KindAssistant, At: at,
					Assistant: &harness.TextBody{Text: clip(text)}})
				if !pending {
					endTurn(harness.StateOK, at)
				}
			}
			if tok := obj(m["tokens"]); tok != nil {
				status = tokenStatus(c.state["model"], tok)
			}
		case "info", "warning", "error":
			level, code := str(m, "type"), harness.NoticeError
			if level == "info" {
				// Info lines are the TUI's own chatter; only a
				// cancellation says something about the turn.
				if !strings.Contains(strings.ToLower(parts(m["content"])), "cancel") {
					continue
				}
				code = harness.NoticeInterrupted
				endTurn(harness.StateInterrupted, at)
			}
			items = append(items, harness.Item{ID: id, TurnID: tid, Kind: harness.KindNotice, At: at,
				Notice: &harness.NoticeBody{Level: level, Code: code, Text: truncate(parts(m["content"]), harness.MaxTitle)}})
			if str(m, "type") == "error" {
				endTurn(harness.StateFailed, at)
			}
		}
	}
	if turn != nil {
		items = append(items, *turn)
		turnOpen = turn.Turn.Ended == nil
	}
	if turn != nil {
		c.state["turn"] = turn.ID
	}
	return items, turnOpen, status
}

func thoughts(v any) string {
	var out []string
	for _, t := range list(v) {
		m := obj(t)
		s := strings.TrimSpace(str(m, "subject"))
		d := strings.TrimSpace(str(m, "description"))
		switch {
		case s != "" && d != "":
			out = append(out, "**"+s+"**\n"+d)
		case s != "" || d != "":
			out = append(out, s+d)
		}
	}
	return strings.Join(out, "\n\n")
}

func tokenStatus(model string, tok map[string]any) *harness.StatusBlock {
	in, _ := num(tok["input"])
	cached, _ := num(tok["cached"])
	out, _ := num(tok["output"])
	return &harness.StatusBlock{Model: model, Facts: []harness.Fact{
		{Label: "Last request", Value: fmt.Sprintf("%.0f in, %.0f cached, %.0f out", in, cached, out)}}}
}

// toolItem maps one recorded tool call; a write_todos call also yields a
// plan item.
func toolItem(tc map[string]any, tid string, at time.Time, cwd string) (*harness.Item, *harness.Item) {
	id := str(tc, "id")
	if id == "" {
		return nil, nil
	}
	name := str(tc, "name")
	args := obj(tc["args"])
	b := &harness.ToolBody{CallID: id, Name: name, Started: stamp(str(tc, "timestamp"), at), State: toolState(str(tc, "status"))}
	describe(b, name, str(tc, "displayName"), args, cwd)
	if b.State != harness.StateRunning && b.State != harness.StateWaiting {
		b.Ended = ptr(b.Started)
	}
	out, _ := tc["resultDisplay"].(string)
	if out == "" {
		for _, r := range list(tc["result"]) {
			resp := obj(obj(obj(r)["functionResponse"])["response"])
			out = strings.TrimSpace(str(resp, "output") + "\n" + str(resp, "error"))
		}
	}
	b.Output = tail(out, harness.MaxToolOutput)
	it := &harness.Item{ID: id, TurnID: tid, Kind: harness.KindTool, At: b.Started, Tool: b}
	if name != "write_todos" {
		return it, nil
	}
	p := &harness.PlanBody{}
	for _, t := range list(args["todos"]) {
		tm := obj(t)
		p.Total++
		switch str(tm, "status") {
		case "completed", "cancelled":
			p.Done++
		case "in_progress":
			if p.Current == "" {
				p.Current = truncate(str(tm, "description"), harness.MaxTitle)
			}
		}
	}
	return it, &harness.Item{ID: "plan-" + id, TurnID: tid, Kind: harness.KindPlan, At: at, Plan: p}
}

func toolState(s string) string {
	switch s {
	case "success":
		return harness.StateOK
	case "error":
		return harness.StateFailed
	case "cancelled":
		return harness.StateCancelled
	case "awaiting_approval":
		return harness.StateWaiting
	}
	return harness.StateRunning
}

// describe fills a tool's title, category, subject and files from Gemini's
// built-in tool names.
func describe(b *harness.ToolBody, name, display string, args map[string]any, cwd string) {
	path := rel(cwd, firstNonEmpty(str(args, "file_path"), str(args, "absolute_path"), str(args, "path"), str(args, "dir_path")))
	cmd := firstLine(str(args, "command"))
	b.Category, b.Title = harness.ToolOther, firstNonEmpty(display, name)
	switch name {
	case "read_file", "read_many_files":
		b.Category, b.Title = harness.ToolRead, "Read "+firstNonEmpty(path, "files")
	case "write_file":
		b.Category, b.Title = harness.ToolWrite, "Write "+path
		b.Files = []harness.FileChange{{Path: path}}
	case "replace", "edit":
		b.Category, b.Title = harness.ToolEdit, "Edit "+path
		b.Files = []harness.FileChange{{Path: path}}
	case "run_shell_command":
		b.Category, b.Title = harness.ToolShell, "Run "+cmd
		if cmd != "" {
			b.Subject = &wire.Subject{Kind: wire.SubjectCommand, Text: truncate(cmd, harness.MaxTitle)}
		}
	case "grep_search", "search_file_content", "glob", "list_directory":
		b.Category, b.Title = harness.ToolSearch, "Search "+firstNonEmpty(str(args, "pattern"), path)
	case "google_web_search", "web_fetch":
		b.Category, b.Title = harness.ToolWeb, "Fetch "+firstNonEmpty(str(args, "query"), str(args, "prompt"), str(args, "url"))
	case "write_todos":
		b.Title = "Update the plan"
	}
	if b.Subject == nil && path != "" {
		b.Subject = &wire.Subject{Kind: wire.SubjectFile, Text: truncate(path, harness.MaxTitle)}
	}
	b.Title = truncate(b.Title, harness.MaxTitle)
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
