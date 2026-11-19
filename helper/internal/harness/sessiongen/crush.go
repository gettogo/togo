package sessiongen

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// Crush writes the project database of Crush 0.100, <cwd>/.crush/crush.db:
// one sqlite file, tables sessions and messages, read read-only by the
// helper with the system sqlite3 and mapped by adapters/crush.
//
// What the adapter reads, and what this profile writes for it:
//
//	sessions  id, title, created_at, updated_at (unix seconds) and
//	          parent_session_id, which the feed filters on (crush.yaml
//	          session feed); the mapper takes the newest top-level row made
//	          since the Togo session began (session.go:122-177)
//	messages  id, session_id, role, parts, created_at, updated_at,
//	          finished_at, is_summary_message (session.go:65-74). Parts
//	          are {type, data}: text, reasoning, tool_call, tool_result and
//	          finish (session.go:77-92). A finish reason of end_turn, stop,
//	          tool_use or error ends or continues the turn
//	          (session.go:325-335); the question tool's call and result are
//	          question.go:13-24 and :153-206.
//
// Shapes are from adapters/crush/testdata: db-sessions.jsonl,
// db-messages.jsonl (a view call), db-questions.jsonl (a question, a
// cancelled question, a denied bash call) and db-errors.jsonl, rows of a
// real 0.100 database. The one shape not from a capture is a finish part with
// reason "canceled", which question_test.go:113 quotes: it is how an
// interrupt would show, and the only cell that uses it is the interrupt.
//
// Not written, because no capture or quoted row holds it: a summary message
// (compaction), a permission prompt (Crush leaves no record while it waits,
// crush.yaml), a question of a type but single_choice, a plan, a model
// change. The profile returns an error for a script that asks for one.
type Crush struct {
	db      sqliteFile
	cwd     string
	sess    []string
	cur     string
	pending func(g *Gen) error
}

func (c *Crush) Name() string { return "crush" }

const crushSchema = `CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, parent_session_id TEXT, title TEXT NOT NULL, message_count INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, parts TEXT NOT NULL DEFAULT '[]', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, finished_at INTEGER, is_summary_message INTEGER DEFAULT 0 NOT NULL);`

func (c *Crush) Open(g *Gen) error {
	c.db = sqliteFile{path: filepath.Join(g.Cwd, ".crush", "crush.db"), schema: crushSchema}
	c.cwd = g.Cwd
	g.L.Mains = append(g.L.Mains, c.db.path)
	return c.start(g)
}

func (c *Crush) start(g *Gen) error {
	at := g.Tick()
	c.cur = uuid(g)
	c.sess = append(c.sess, c.cur)
	c.db.add("INSERT INTO sessions (id, parent_session_id, title, created_at, updated_at) VALUES (%s, NULL, 'Untitled Session', %d, %d)", sq(c.cur), at.Unix(), at.Unix())
	return nil
}

func part(typ string, data map[string]any) map[string]any {
	return map[string]any{"type": typ, "data": data}
}

func finish(reason string, at int64) map[string]any {
	d := map[string]any{"reason": reason, "time": at}
	if reason == "stop" {
		d["time"] = 0
	}
	return part("finish", d)
}

// row queues a message row; finished is the finish time or 0 for none.
func (c *Crush) row(g *Gen, role string, parts []any, created, updated, finished int64) string {
	id := uuid(g)
	raw, _ := json.Marshal(parts)
	fin := "NULL"
	if finished > 0 {
		fin = fmt.Sprint(finished)
	}
	c.db.add("INSERT INTO messages (id, session_id, role, parts, created_at, updated_at, finished_at) VALUES (%s, %s, %s, %s, %d, %d, %s)",
		sq(id), sq(c.cur), sq(role), sq(string(raw)), created, updated, fin)
	c.db.add("UPDATE sessions SET updated_at = %d WHERE id = %s", updated, sq(c.cur))
	return id
}

func (c *Crush) Turn(g *Gen, t Turn) error {
	h := t.Hook
	if h == nil {
		h = &HookTurn{}
	}
	switch {
	case t.Compact:
		return fmt.Errorf("sessiongen: crush: no capture holds a summary message")
	case t.Jobs > 0 || t.Agents != nil:
		return fmt.Errorf("sessiongen: crush has no background job or subagent row")
	case h.Model != "" || h.Steer || h.Aborted || h.NoStop || h.Approve > 0:
		return fmt.Errorf("sessiongen: crush: its permission prompts leave no record, and no capture holds a model change or a queued prompt")
	case h.Ask != nil && h.Ask.Plan != "":
		return fmt.Errorf("sessiongen: crush has no plan approval record")
	}
	switch {
	case h.Resume && len(c.sess) > 1:
		c.cur = c.sess[len(c.sess)-2]
	case t.New:
		if err := c.start(g); err != nil {
			return err
		}
	}
	if t.Prompt == Continue {
		if c.pending == nil {
			return fmt.Errorf("sessiongen: crush has no open turn to continue")
		}
		f := c.pending
		c.pending = nil
		if err := f(g); err != nil {
			return err
		}
		return c.end(g, t)
	}
	c.pending = nil
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts+1, g.Text(40))
	}
	g.L.Truth.Prompts++
	at := g.Tick().Unix()
	c.row(g, "user", []any{part("text", map[string]any{"text": prompt}), finish("stop", 0)}, at, at, 0)
	for i := 0; i < t.Tools; i++ {
		if err := c.tool(g, t, h, i); err != nil {
			return err
		}
		if c.pending != nil {
			return nil
		}
	}
	if a := h.Ask; a != nil {
		if err := c.ask(g, a, h.Pending); err != nil {
			return err
		}
		if c.pending != nil {
			return nil
		}
	}
	return c.end(g, t)
}

func (c *Crush) tool(g *Gen, t Turn, h *HookTurn, i int) error {
	call := g.Hex(32)
	at := g.Tick().Unix()
	input, _ := json.Marshal(map[string]any{"command": "echo " + g.Hex(4), "description": "Run echo"})
	c.row(g, "assistant", []any{
		part("reasoning", map[string]any{"thinking": g.Text(50), "signature": "", "thought_signature": "", "tool_id": "", "responses_data": nil, "started_at": at, "finished_at": at}),
		part("tool_call", map[string]any{"id": call, "name": "bash", "input": string(input), "provider_executed": false, "finished": true}),
		finish("tool_use", at)}, at, at, at)
	answer := func(g *Gen) error {
		at := g.Tick().Unix()
		out, isErr := g.Text(max(t.Output, h.Large, 1)), false
		if i < h.Fail {
			out, isErr = "User denied permission", true
		}
		c.row(g, "tool", []any{part("tool_result", map[string]any{"tool_call_id": call, "name": "bash", "content": out, "data": "", "mime_type": "", "metadata": "", "is_error": isErr}),
			finish("stop", 0)}, at, at, 0)
		return nil
	}
	if h.Pending && i == t.Tools-1 && h.Ask == nil {
		c.pending = answer
		g.L.Truth.Last = call
		return nil
	}
	return answer(g)
}

// ask writes the question tool: a single_choice question, answered or
// cancelled, or left open (the assistant row has no finish part then).
func (c *Crush) ask(g *Gen, a *HookAsk, open bool) error {
	call := g.Hex(32)
	choices := make([]any, 0, len(a.Options))
	pick := ""
	for _, o := range a.Options {
		id := "c_" + o
		if o == a.Answer {
			pick = id
		}
		choices = append(choices, map[string]any{"id": id, "label": o, "description": "Option " + o})
	}
	in, _ := json.Marshal(map[string]any{"questions": []any{map[string]any{"description": "A question.", "question": a.Prompt, "type": "single_choice", "choices": choices}}})
	at := g.Tick().Unix()
	call1 := part("tool_call", map[string]any{"id": call, "name": "question", "input": string(in), "provider_executed": false, "finished": true})
	parts := []any{part("reasoning", map[string]any{"thinking": g.Text(40), "signature": "", "thought_signature": "", "tool_id": "", "responses_data": nil, "started_at": at, "finished_at": at}), call1}
	if open {
		c.row(g, "assistant", parts, at, at, 0)
		c.pending = func(g *Gen) error { return c.answer(g, call, a, pick) }
		g.L.Truth.Last = call
		return nil
	}
	c.row(g, "assistant", append(parts, finish("tool_use", at)), at, at, at)
	return c.answer(g, call, a, pick)
}

func (c *Crush) answer(g *Gen, call string, a *HookAsk, pick string) error {
	at := g.Tick().Unix()
	content, isErr := fmt.Sprintf("Q1: %s\nUser selected: [%q]", a.Prompt, pick), false
	if a.Cancel {
		content, isErr = "User cancelled this question", true
	}
	c.row(g, "tool", []any{part("tool_result", map[string]any{"tool_call_id": call, "name": "question", "content": content, "data": "", "mime_type": "", "metadata": "", "is_error": isErr}),
		finish("stop", 0)}, at, at, 0)
	return nil
}

// end writes the last message of the turn.
func (c *Crush) end(g *Gen, t Turn) error {
	h := t.Hook
	if h == nil {
		h = &HookTurn{}
	}
	start := g.Tick().Unix()
	at := g.Tick().Unix()
	var id string
	switch {
	case t.Interrupt:
		id = c.row(g, "assistant", []any{part("reasoning", map[string]any{"thinking": g.Text(30), "signature": "", "thought_signature": "", "tool_id": "", "responses_data": nil, "started_at": start, "finished_at": at}),
			map[string]any{"type": "finish", "data": map[string]any{"reason": "canceled"}}}, start, at, at)
		g.L.Truth.Last = id + ":thought"
	case t.Error:
		details := firstNonEmpty(h.ErrorText, `retry error: Post "http://127.0.0.1:9/v1/chat/completions": dial tcp 127.0.0.1:9: connect: connection refused`)
		id = c.row(g, "assistant", []any{part("finish", map[string]any{"reason": "error", "time": at, "message": "Provider Error", "details": details})}, start, at, at)
		g.L.Truth.Last = id + ":error"
	default:
		id = c.row(g, "assistant", []any{
			part("reasoning", map[string]any{"thinking": g.Text(40), "signature": "", "thought_signature": "", "tool_id": "", "responses_data": nil, "started_at": start, "finished_at": at}),
			part("text", map[string]any{"text": g.Text(max(t.Say, 1))}), finish("end_turn", at)}, start, at, at)
		g.L.Truth.Last = id + ":text"
	}
	if t.Exit {
		g.L.Truth.Exited = true
	}
	return nil
}

// Flushed writes the queued rows.
func (c *Crush) Flushed(g *Gen) error { return c.db.flush() }
