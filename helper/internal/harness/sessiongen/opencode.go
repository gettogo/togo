package sessiongen

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"
)

// OpenCode writes the event stream of OpenCode 1.x's server, and of Kilo
// Code's (an OpenCode fork serving the same /event stream), as a journal
// of one event per line. Neither agent keeps a file the helper reads: the
// helper finds the server on the port it picked at launch (engine/
// endpoint.go EndpointArgs: `--port N`, a fresh password in
// OPENCODE_SERVER_PASSWORD or KILO_SERVER_PASSWORD, Basic auth as
// "opencode" or "kilo"; opencode.yaml:10, kilo.yaml:14) and reads it over
// HTTP. The test API server (enginetest.APIServer) plays the journal on
// /event as the agent's stream and answers the routes the adapter reads
// from what the journal holds.
//
// What the helper reads and sends (adapters/opencode, the 1.x adapter;
// Kilo's adapter file is the same with its own ids, kilo.yaml):
//   - feed: GET /event, server-sent events, reconnecting with backoff
//     (source/httpsrc/sse.go:192); a reconnect is reported to the mapper
//     as stream.reconnected (sse.go:232), which closes open questions
//     (events.go:141). The stream has no history: events sent while the
//     helper is away are lost, and 1.x has no history route (opencode.yaml
//     feeds has no history).
//   - mapper events (events.go:94-155): session.created/updated (the main
//     session, its model and agent; a session with a parentID is a
//     subagent), tui.session.select, message.updated (a user message
//     starts a turn and, in another top-level session, moves the chat
//     there: events.go:291), message.part.updated and .delta (text,
//     reasoning, tools: parts.go), session.status (busy, idle, retry,
//     offline), session.idle, session.diff (changes), todo.updated (plan),
//     question.asked/replied/rejected, permission.asked/replied,
//     session.compacted, session.error.
//   - sources: GET /session (resume list), /config/providers (models,
//     providers), /provider, /agent, /command, /mcp, and per session
//     /session/{id}/todo, /children, /diff (opencode.yaml:20-73).
//   - actions, all API calls, none typed: send is POST
//     /session/{id}/prompt_async; interrupt POST /session/{id}/abort;
//     answers POST /permission/{id}/reply ({"reply"}) or
//     /question/{id}/reply ({"answers"}); model POST
//     /api/session/{id}/model; mode POST /api/session/{id}/agent; a /
//     line POST /session/{id}/command; compact POST
//     /session/{id}/summarize; resume POST /tui/select-session; a start
//     before the first prompt POST /session (opencode.yaml:110-170).
//
// Each event carries only fields that the helper reads and that are found
// in the real recordings (adapters/opencode/testdata/events.sse, OpenCode
// 1.18.20;
// testdata/kilo/offline.jsonl and adapters/kilo/testdata/events.sse,
// Kilo 7.8.27), with two exceptions quoted from the adapter's tests: a
// child session's parentID (events_test.go:213) and a diff entry's file,
// additions and deletions (revert2_test.go:117; the recordings' diffs are
// all empty). honesty_opencode_test.go checks every event against them.
//
// Not written, for want of a real recording: a task tool call (subagent
// sessions appear on their own, as session.created with a parentID), an
// aborted message (MessageAbortedError), session.compacted, and
// background shells (1.x has none).
type OpenCode struct {
	// Kilo writes Kilo Code's stream: its version and default agent.
	Kilo bool
	// Mode, Provider and Model, when set, are the agent and model the
	// following turns run with, as after a switch.
	Mode, Provider, Model string

	main    string // the session the terminal shows
	journal string
	ask     *openAsk
}

// openAsk is a turn waiting on a permission or a question.
type openAsk struct {
	user, msg, part, call, cmd string
	start                      time.Time
}

// OpenCodeJournal is where the OpenCode and Kilo profiles write the
// event journal under home: one event per line, as the server streams
// them. It is test data, not a file the agent keeps.
func OpenCodeJournal(home string) string {
	return filepath.Join(home, ".togo-test", "server-events.jsonl")
}

// Name is the adapter's name.
func (p *OpenCode) Name() string {
	if p.Kilo {
		return "kilo"
	}
	return "opencode"
}

// Session is the id of the session the terminal shows now.
func (p *OpenCode) Session() string { return p.main }

// Select makes the terminal show session id, as a resume does: the turns
// written after it go to that session.
func (p *OpenCode) Select(id string) { p.main = id }

// Agent is the agent (Togo's mode) the session runs: Kilo's default is
// "code" (offline.jsonl), OpenCode's "build" (events.sse).
func (p *OpenCode) Agent() string {
	if p.Mode != "" {
		return p.Mode
	}
	if p.Kilo {
		return "code"
	}
	return "build"
}

// Provider and Model name the model of the recordings.
const (
	OpenCodeProvider = "local"
	OpenCodeModel    = "qwen3.8-27b"
)

func (p *OpenCode) model() (string, string) {
	if p.Model != "" {
		return p.Provider, p.Model
	}
	return OpenCodeProvider, OpenCodeModel
}

// ocID makes an OpenCode id: a prefix, then hex that grows with time, as
// OpenCode's ids do (so sorted ids are in the order they were made).
func ocID(g *Gen, prefix string) string {
	return prefix + "_" + fmt.Sprintf("%012x", g.Serial()) + g.Hex(14)
}

func (p *OpenCode) emit(g *Gen, at time.Time, typ string, props map[string]any) error {
	return g.Emit(p.journal, at, map[string]any{"type": typ, "properties": props})
}

// Open writes the session's start: created, then updated with its agent
// and model, as the first prompt makes it.
func (p *OpenCode) Open(g *Gen) error {
	p.journal = OpenCodeJournal(g.Home)
	g.L.Mains = []string{p.journal}
	_, err := p.newSession(g, "", "")
	return err
}

// sessionInfo is a session as session.created and the /session routes
// give it.
func (p *OpenCode) sessionInfo(g *Gen, id, parent, title string, at time.Time) map[string]any {
	ms := at.UnixMilli()
	info := map[string]any{"id": id, "title": title, "time": map[string]any{"created": ms, "updated": ms}}
	if parent != "" {
		info["parentID"] = parent
		info["agent"] = "general"
	}
	return info
}

// newSession writes a session's creation. A top-level one becomes the
// one the terminal shows, with its agent and model set. It returns the
// session's id.
func (p *OpenCode) newSession(g *Gen, parent, title string) (string, error) {
	id := ocID(g, "ses")
	at := g.Tick()
	if title == "" {
		title = "New session - " + at.Format("2006-01-02T15:04:05.000Z")
	}
	info := p.sessionInfo(g, id, parent, title, at)
	if err := p.emit(g, at, "session.created", map[string]any{"sessionID": id, "info": info}); err != nil {
		return "", err
	}
	if parent != "" {
		return id, nil
	}
	p.main = id
	upd := p.sessionInfo(g, id, "", title, at)
	upd["agent"] = p.Agent()
	prov, model := p.model()
	upd["model"] = map[string]any{"id": model, "providerID": prov}
	return id, p.emit(g, g.Tick(), "session.updated", map[string]any{"sessionID": id, "info": upd})
}

// Turn writes one prompt and what the agent does with it.
func (p *OpenCode) Turn(g *Gen, t Turn) error {
	switch {
	case t.Compact:
		return errors.New("sessiongen: no real recording of an OpenCode compaction")
	case t.Error:
		return errors.New("sessiongen: no real recording of a failed OpenCode turn")
	case t.Jobs > 0:
		return errors.New("sessiongen: OpenCode 1.x has no background shells")
	case t.Agents != nil && t.Agents.Background:
		return errors.New("sessiongen: OpenCode runs its subagents inside the turn")
	}
	if t.New {
		if _, err := p.newSession(g, "", ""); err != nil {
			return err
		}
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = "Step " + strconv.Itoa(g.L.Truth.Prompts+1) + ": " + g.Text(40)
	}
	g.L.Truth.Prompts++
	user, err := p.userMessage(g, p.main, prompt)
	if err != nil {
		return err
	}
	if err := p.status(g, p.main, "busy"); err != nil {
		return err
	}
	asst, err := p.assistant(g, p.main, user, false)
	if err != nil {
		return err
	}
	for i := 0; i < t.Tools; i++ {
		if err := p.tool(g, p.main, asst, t.Output); err != nil {
			return err
		}
	}
	if t.Tools > 0 {
		if err := p.planAndDiff(g, g.L.Truth.Prompts); err != nil {
			return err
		}
	}
	if t.Agents != nil {
		if err := p.children(g, t.Agents); err != nil {
			return err
		}
	}
	if !t.Interrupt {
		last, err := p.text(g, p.main, asst, max(t.Say, 1))
		if err != nil {
			return err
		}
		g.L.Truth.Last = last
	}
	if _, err := p.assistant(g, p.main, user, true, asst); err != nil {
		return err
	}
	return p.idle(g, p.main)
}

// userMessage writes a prompt: the user message and its text part. The
// id is the turn's.
func (p *OpenCode) userMessage(g *Gen, sess, text string) (string, error) {
	id := ocID(g, "msg")
	at := g.Tick()
	prov, model := p.model()
	info := map[string]any{"id": id, "role": "user", "sessionID": sess, "time": map[string]any{"created": at.UnixMilli()},
		"agent": p.Agent(), "model": map[string]any{"providerID": prov, "modelID": model}}
	if err := p.emit(g, at, "message.updated", map[string]any{"sessionID": sess, "info": info}); err != nil {
		return "", err
	}
	part := map[string]any{"type": "text", "text": text, "messageID": id, "sessionID": sess, "id": ocID(g, "prt")}
	return id, p.emit(g, g.Tick(), "message.part.updated", map[string]any{"sessionID": sess, "part": part})
}

func (p *OpenCode) status(g *Gen, sess, typ string) error {
	return p.emit(g, g.Tick(), "session.status", map[string]any{"sessionID": sess, "status": map[string]any{"type": typ}})
}

// assistant writes the assistant message of a turn, created or (done)
// completed. A completed one names the id it completes.
func (p *OpenCode) assistant(g *Gen, sess, parent string, done bool, id ...string) (string, error) {
	at := g.Tick()
	mid := ""
	if len(id) > 0 {
		mid = id[0]
	} else {
		mid = ocID(g, "msg")
	}
	tm := map[string]any{"created": at.UnixMilli()}
	prov, model := p.model()
	info := map[string]any{"id": mid, "parentID": parent, "role": "assistant", "mode": p.Agent(), "agent": p.Agent(),
		"modelID": model, "providerID": prov, "sessionID": sess, "time": tm}
	if done {
		tm["completed"] = at.UnixMilli()
	}
	if err := p.emit(g, at, "message.updated", map[string]any{"sessionID": sess, "info": info}); err != nil {
		return "", err
	}
	if done {
		return mid, nil
	}
	step := map[string]any{"id": ocID(g, "prt"), "messageID": mid, "sessionID": sess, "type": "step-start"}
	return mid, p.emit(g, g.Tick(), "message.part.updated", map[string]any{"sessionID": sess, "part": step})
}

// tool writes one bash call: pending, running, completed.
func (p *OpenCode) tool(g *Gen, sess, msg string, size int) error {
	id, call := ocID(g, "prt"), g.Hex(32)
	cmd := "echo step-" + strconv.Itoa(g.Serial())
	out := g.Text(max(size, 1)) + "\n"
	start := g.Tick()
	states := []map[string]any{
		{"status": "pending", "input": map[string]any{}, "raw": ""},
		{"status": "running", "input": map[string]any{"command": cmd}, "time": map[string]any{"start": start.UnixMilli()}},
	}
	end := g.Tick()
	states = append(states, map[string]any{"status": "completed", "input": map[string]any{"command": cmd}, "output": out,
		"metadata": map[string]any{"output": out, "exit": 0, "truncated": false}, "title": cmd,
		"time": map[string]any{"start": start.UnixMilli(), "end": end.UnixMilli()}})
	for _, st := range states {
		part := map[string]any{"id": id, "messageID": msg, "sessionID": sess, "type": "tool", "tool": "bash", "callID": call, "state": st}
		if err := p.emit(g, g.Tick(), "message.part.updated", map[string]any{"sessionID": sess, "part": part}); err != nil {
			return err
		}
	}
	return nil
}

// text writes a reply streamed in pieces: the part, its deltas, the part
// with its whole text. It returns the part id.
func (p *OpenCode) text(g *Gen, sess, msg string, size int) (string, error) {
	id := ocID(g, "prt")
	whole := g.Text(size)
	start := g.Tick()
	part := map[string]any{"id": id, "messageID": msg, "sessionID": sess, "type": "text", "text": "", "time": map[string]any{"start": start.UnixMilli()}}
	if err := p.emit(g, start, "message.part.updated", map[string]any{"sessionID": sess, "part": part}); err != nil {
		return "", err
	}
	for i := 0; i < len(whole); i += 64 {
		d := whole[i:min(i+64, len(whole))]
		if err := p.emit(g, g.Tick(), "message.part.delta", map[string]any{"sessionID": sess, "partID": id, "field": "text", "delta": d}); err != nil {
			return "", err
		}
	}
	end := g.Tick()
	full := map[string]any{"id": id, "messageID": msg, "sessionID": sess, "type": "text", "text": whole,
		"time": map[string]any{"start": start.UnixMilli(), "end": end.UnixMilli()}}
	if err := p.emit(g, end, "message.part.updated", map[string]any{"sessionID": sess, "part": full}); err != nil {
		return "", err
	}
	fin := map[string]any{"id": ocID(g, "prt"), "messageID": msg, "sessionID": sess, "type": "step-finish"}
	return id, p.emit(g, g.Tick(), "message.part.updated", map[string]any{"sessionID": sess, "part": fin})
}

// planAndDiff writes the plan the turn keeps (todo.updated) and the
// files it changed (session.diff).
func (p *OpenCode) planAndDiff(g *Gen, n int) error {
	todos := []map[string]any{
		{"content": "check " + strconv.Itoa(n), "status": "completed"},
		{"content": "report " + strconv.Itoa(n), "status": "pending"},
	}
	if err := p.emit(g, g.Tick(), "todo.updated", map[string]any{"sessionID": p.main, "todos": todos}); err != nil {
		return err
	}
	diff := []map[string]any{{"file": filepath.Join(g.Cwd, "notes-"+strconv.Itoa(n)+".txt"), "additions": n, "deletions": 1}}
	return p.emit(g, g.Tick(), "session.diff", map[string]any{"sessionID": p.main, "diff": diff})
}

// children writes a batch of subagent sessions, each running its steps
// and going idle, in a seeded order, inside the main turn.
func (p *OpenCode) children(g *Gen, b *Batch) error {
	for _, i := range g.Shuffle(b.Size) {
		title := "Explore part " + strconv.Itoa(i+1) + " (@general subagent)"
		child, err := p.newSession(g, p.main, title)
		if err != nil {
			return err
		}
		prompt := "Look at part " + strconv.Itoa(i+1)
		u, err := p.userMessage(g, child, prompt)
		if err != nil {
			return err
		}
		a, err := p.assistant(g, child, u, false)
		if err != nil {
			return err
		}
		for s := 0; s < b.Steps; s++ {
			if err := p.tool(g, child, a, 40); err != nil {
				return err
			}
		}
		if _, err := p.text(g, child, a, 30); err != nil {
			return err
		}
		if _, err := p.assistant(g, child, u, true, a); err != nil {
			return err
		}
		if err := p.idle(g, child); err != nil {
			return err
		}
		g.L.Truth.Agents = append(g.L.Truth.Agents, AgentTruth{Task: child, Prompt: prompt, File: p.journal, Exited: true, Settled: true})
	}
	return nil
}

func (p *OpenCode) idle(g *Gen, sess string) error {
	if err := p.status(g, sess, "idle"); err != nil {
		return err
	}
	return p.emit(g, g.Tick(), "session.idle", map[string]any{"sessionID": sess})
}

// OpenCodeEvent decodes one journal line's type and properties.
func OpenCodeEvent(line []byte) (string, json.RawMessage, error) {
	var ev struct {
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
	}
	err := json.Unmarshal(line, &ev)
	return ev.Type, ev.Properties, err
}

// Ask starts a turn that stops on a permission (question false) or on the
// question tool: a prompt, a bash call running, then permission.asked or
// question.asked naming that call, as in events.sse. It returns the
// request's id. EndAsk finishes the turn.
func (p *OpenCode) Ask(l *Layout, question bool) (string, error) {
	g := l.g
	if p.ask != nil {
		return "", errors.New("sessiongen: a turn already waits")
	}
	g.L.Truth.Prompts++
	user, err := p.userMessage(g, p.main, "Step "+strconv.Itoa(g.L.Truth.Prompts)+": "+g.Text(30))
	if err != nil {
		return "", err
	}
	if err := p.status(g, p.main, "busy"); err != nil {
		return "", err
	}
	msg, err := p.assistant(g, p.main, user, false)
	if err != nil {
		return "", err
	}
	a := &openAsk{user: user, msg: msg, part: ocID(g, "prt"), call: g.Hex(32), cmd: "cat /etc/hosts", start: g.Tick()}
	part := map[string]any{"id": a.part, "messageID": msg, "sessionID": p.main, "type": "tool", "tool": "bash", "callID": a.call,
		"state": map[string]any{"status": "running", "input": map[string]any{"command": a.cmd}, "time": map[string]any{"start": a.start.UnixMilli()}}}
	if err := p.emit(g, g.Tick(), "message.part.updated", map[string]any{"sessionID": p.main, "part": part}); err != nil {
		return "", err
	}
	tool := map[string]any{"messageID": msg, "callID": a.call}
	var id string
	if question {
		id = ocID(g, "que")
		qs := []map[string]any{{"question": "Which colour do you prefer?", "header": "Colour preference",
			"options": []map[string]any{{"label": "red", "description": "You prefer red"}, {"label": "blue", "description": "You prefer blue"}}}}
		err = p.emit(g, g.Tick(), "question.asked", map[string]any{"id": id, "sessionID": p.main, "questions": qs, "tool": tool})
	} else {
		id = ocID(g, "per")
		err = p.emit(g, g.Tick(), "permission.asked", map[string]any{"id": id, "sessionID": p.main, "permission": "external_directory",
			"patterns": []string{"/etc/*"}, "metadata": map[string]any{"filepath": "/etc/hosts", "parentDir": "/etc"}, "tool": tool})
	}
	if err != nil {
		return "", err
	}
	p.ask = a
	return id, g.flush()
}

// EndAsk finishes the turn Ask started: the call completes, the reply
// follows and the session goes idle.
func (p *OpenCode) EndAsk(l *Layout) error {
	g, a := l.g, p.ask
	if a == nil {
		return errors.New("sessiongen: no turn waits")
	}
	p.ask = nil
	end := g.Tick()
	out := "127.0.0.1 localhost\n"
	part := map[string]any{"id": a.part, "messageID": a.msg, "sessionID": p.main, "type": "tool", "tool": "bash", "callID": a.call,
		"state": map[string]any{"status": "completed", "input": map[string]any{"command": a.cmd}, "output": out,
			"metadata": map[string]any{"output": out, "exit": 0, "truncated": false}, "title": a.cmd,
			"time": map[string]any{"start": a.start.UnixMilli(), "end": end.UnixMilli()}}}
	if err := p.emit(g, end, "message.part.updated", map[string]any{"sessionID": p.main, "part": part}); err != nil {
		return err
	}
	last, err := p.text(g, p.main, a.msg, 30)
	if err != nil {
		return err
	}
	g.L.Truth.Last = last
	if _, err := p.assistant(g, p.main, a.user, true, a.msg); err != nil {
		return err
	}
	if err := p.idle(g, p.main); err != nil {
		return err
	}
	return g.flush()
}
