package sessiongen

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Cline writes Cline's session files: ~/.cline/data/sessions/<id>/
// <id>.messages.json, a whole document that Cline rewrites after each
// assistant message and loop iteration, and the manifest <id>.json next to
// it (adapters/cline/cline.yaml, messages.go). The generator keeps the
// conversation and rewrites the document once per Append (Finisher).
//
// What the mapper reads, from adapters/cline/messages.go and ask.go:
//   - the document: updated_at, agent, sessionId, messages[];
//   - a message: id, role, content (text, thinking, tool_use, tool_result
//     blocks), ts, metadata.displayOnly and metadata.displayRole "error"
//     for the error Cline appends when a run fails;
//   - a tool call: editor, run_commands, ask_question, the tools of the
//     recorded sessions; a tool_result's content is a string or a list;
//   - the manifest: cwd, started_at, model, metadata.mode, interactive
//     (true, as the TUI writes it: testdata/ask.json). The recorded
//     manifests carry no pid, so none is written.
//
// Fields the mapper does not read (version, origin, modelInfo, metrics,
// the manifest's status, provider and source) are left out.
//
// Cline writes no record of its exit or of an interrupt (none is
// recorded), so Exit, Interrupt, Agents and Jobs are refused. A /new
// writes a new session folder.
type Cline struct {
	// Extra adds Cline-only content to a turn, keyed by the number of the
	// turn's prompt (1 is the first).
	Extra map[int]ClineTurn

	id    string
	mode  string
	docs  map[string]*clineDoc // by messages file
	cur   *clineDoc
	files []*clineDoc
}

// ClineTurn is what a Cline turn can have besides the common script
// fields.
type ClineTurn struct {
	// Edits is the number of editor calls before the reply; each writes a
	// file.
	Edits int
	// Commands is the number of run_commands calls before the reply.
	Commands int
	// Think writes a thinking block in the first step.
	Think bool
	// Question raises an ask_question call.
	Question *ClineQuestion
	// Mode is the mode of the prompt's <user_input> ("plan" or "act");
	// empty keeps the last.
	Mode string
	// Unfinished ends the document after a tool call with no result and
	// no reply, as when Cline is killed at work.
	Unfinished bool
}

// ClineQuestion is an ask_question call.
type ClineQuestion struct {
	Question string
	Options  []string
	Answer   string
	// Cancelled answers it with an error result, as Esc does; Open leaves
	// it unanswered, the turn waiting.
	Cancelled, Open bool
}

type clineDoc struct {
	id, path, manifest string
	started            time.Time
	msgs               []any
	last               time.Time
	dirty              bool
}

func (c *Cline) Name() string { return "cline" }

func (c *Cline) Open(g *Gen) error { return c.newSession(g) }

func (c *Cline) newSession(g *Gen) error {
	if c.docs == nil {
		c.docs = map[string]*clineDoc{}
	}
	at := g.Tick()
	c.id = fmt.Sprintf("%d_%s", at.UnixMilli(), g.Hex(5))
	dir := filepath.Join(g.Home, ".cline", "data", "sessions", c.id)
	d := &clineDoc{id: c.id, path: filepath.Join(dir, c.id+".messages.json"), manifest: filepath.Join(dir, c.id+".json"), started: at, last: at, dirty: true}
	c.docs[d.path], c.cur = d, d
	c.files = append(c.files, d)
	g.L.Mains = append(g.L.Mains, d.path)
	if c.mode == "" {
		c.mode = "act"
	}
	return nil
}

func (c *Cline) add(g *Gen, m map[string]any) {
	at := g.Tick()
	m["ts"] = at.UnixMilli()
	c.cur.msgs = append(c.cur.msgs, m)
	c.cur.last, c.cur.dirty = at, true
	g.L.Records[c.cur.path]++
}

func (c *Cline) assistant(g *Gen, blocks ...any) map[string]any {
	return map[string]any{"id": "msg_" + g.Hex(8), "role": "assistant", "content": blocks}
}

func (c *Cline) result(g *Gen, id, name string, content any, failed bool) map[string]any {
	b := map[string]any{"type": "tool_result", "tool_use_id": id, "name": name, "content": content}
	if failed {
		b["is_error"] = true
	}
	return map[string]any{"id": "msg_" + g.Hex(8), "role": "user", "content": []any{b}}
}

func use(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func (c *Cline) Turn(g *Gen, t Turn) error {
	if t.Agents != nil || t.Jobs > 0 || t.Exit || t.Interrupt || t.Compact {
		return fmt.Errorf("sessiongen: the Cline profile writes no subagents, jobs, exit, interrupt or compaction: no real recording of them")
	}
	if t.New {
		if err := c.newSession(g); err != nil {
			return err
		}
	}
	n := g.L.Truth.Prompts + 1
	x := c.Extra[n]
	if x.Mode != "" {
		c.mode = x.Mode
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", n, g.Text(40))
	}
	g.L.Truth.Prompts++
	c.add(g, map[string]any{"id": uuid(g), "role": "user", "content": []any{map[string]any{"type": "text",
		"text": `<user_input mode="` + c.mode + `">` + prompt + `</user_input>`}}})
	first := true
	step := func(blocks ...any) map[string]any {
		if first && x.Think {
			blocks = append([]any{map[string]any{"type": "thinking", "thinking": g.Text(20)}}, blocks...)
		}
		first = false
		return c.assistant(g, blocks...)
	}
	for i := 0; i < t.Tools; i++ {
		id := g.Hex(16) + g.Hex(16)
		c.add(g, step(use(id, "run_commands", map[string]any{"commands": []any{"echo " + g.Hex(4)}})))
		c.add(g, c.result(g, id, "run_commands", []any{map[string]any{"query": "echo", "result": g.Text(max(t.Output, 1)), "success": true}}, false))
	}
	for i := 0; i < x.Commands; i++ {
		id := g.Hex(16) + g.Hex(16)
		c.add(g, step(use(id, "run_commands", map[string]any{"commands": []any{"make test"}})))
		c.add(g, c.result(g, id, "run_commands", []any{map[string]any{"query": "make test", "result": "", "success": true}}, false))
	}
	for i := 0; i < x.Edits; i++ {
		id, file := g.Hex(16)+g.Hex(16), filepath.Join(g.Cwd, fmt.Sprintf("e%d_%d.txt", n, i))
		c.add(g, step(use(id, "editor", map[string]any{"path": file, "new_text": g.Text(10)})))
		out, _ := json.Marshal(map[string]any{"query": "edit:" + file, "result": "File created successfully at: " + file, "success": true})
		c.add(g, c.result(g, id, "editor", string(out), false))
	}
	if q := x.Question; q != nil {
		id := g.Hex(16) + g.Hex(16)
		opts := make([]any, len(q.Options))
		for i, o := range q.Options {
			opts[i] = o
		}
		c.add(g, step(use(id, "ask_question", map[string]any{"question": q.Question, "options": opts})))
		switch {
		case q.Open:
			return nil
		case q.Cancelled:
			c.add(g, c.result(g, id, "ask_question", "The user did not answer the question.", true))
		default:
			c.add(g, c.result(g, id, "ask_question", q.Answer, false))
		}
	}
	switch {
	case x.Unfinished:
		c.add(g, step(use(g.Hex(16)+g.Hex(16), "run_commands", map[string]any{"commands": []any{"go test ./..."}})))
	case t.Error:
		// What Cline appends when a run fails (3.0.74 recordTerminalError).
		m := c.assistant(g, map[string]any{"type": "text", "text": "401 Incorrect API key provided"})
		m["id"] = "error_" + uuid(g)
		m["metadata"] = map[string]any{"displayOnly": true, "displayRole": "error"}
		c.add(g, m)
	default:
		c.add(g, step(map[string]any{"type": "text", "text": g.Text(max(t.Say, 1))}))
	}
	return nil
}

// Finish rewrites the documents that changed, and each one's manifest.
func (c *Cline) Finish(g *Gen) error {
	for _, d := range c.files {
		if !d.dirty {
			continue
		}
		d.dirty = false
		doc := map[string]any{"updated_at": stamp(d.last), "agent": "lead", "sessionId": d.id, "messages": d.msgs}
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		man, err := json.MarshalIndent(map[string]any{"started_at": stamp(d.started), "model": "coder-fast",
			"cwd": g.Cwd, "interactive": true, "metadata": map[string]any{"mode": c.mode}}, "", "  ")
		if err != nil {
			return err
		}
		if err := g.Replace(d.manifest, d.started, strings.TrimSpace(string(man))+"\n"); err != nil {
			return err
		}
		if err := g.Replace(d.path, d.last, string(raw)+"\n"); err != nil {
			return err
		}
	}
	return nil
}
