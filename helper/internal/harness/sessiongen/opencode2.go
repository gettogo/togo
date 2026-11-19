package sessiongen

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// OpenCode2 writes the event stream of OpenCode 2.x's server as a journal
// of one event per line (the same file as the 1.x profile,
// OpenCodeJournal). 2.x is a rewrite: the terminal is a client of a server
// the helper starts for the session, and the helper reads that server.
//
// How the helper serves a 2.x session (adapters/opencode/opencode2.yaml,
// the mapper events2.go; the engine under harness/engine):
//   - Which file: the engine picks opencode2.yaml over opencode.yaml when
//     `opencode --version` is in ">=2 <3" (opencode2.yaml:14;
//     engine/endpoint.go:313-333 adapterFor).
//   - The server: at launch the engine picks a free port and a fresh
//     password and starts `opencode serve --hostname 127.0.0.1 --port N`
//     itself, in its own process group, with OPENCODE_PASSWORD set
//     (opencode2.yaml:15-23; engine/endpoint.go:150-205, engine/server.go:
//     41-105), waits for GET /api/info to answer (server.go:100, 140-198),
//     then starts the terminal with `--server http://127.0.0.1:N`
//     (endpoint.go:188-197). The port, password and the server's process
//     identity are written to $TOGO_HOME/run/harness/<session>.json
//     (engine/recovery.go:182-210). A restarted helper finds the same
//     server there: the process must still lead its group and answer its
//     health check (recovery.go:212-300), and it is adopted, not started
//     again. A relaunch (resume, show_session) calls EndpointArgs again:
//     the old server is stopped and a new one starts on a new port and
//     password (endpoint.go:140-149, server.go:42, 107-136), and the
//     mapper starts over (endpoint.go:222-257).
//   - The stream: GET /api/event, server-sent events {id, created, type,
//     location, data} (opencode2.yaml:40; events2.go:94-104). It has no
//     past: a reconnect is reported to the mapper (httpsrc Reconnected),
//     which forgets whether the turn runs, closes open questions and
//     renews the command and agent lists (events2.go:197-202, 491-534).
//   - History: when the chat learns its conversation, moves to another,
//     or after a helper restart (the conversation kept in
//     run/harness/<session>.conversation), GET
//     /api/session/{id}/message?limit=200&order=desc, newest page first,
//     older pages by `.cursor.next` sent as `cursor` without `order`
//     (opencode2.yaml:46-52; engine/history.go:19-31, 193-307). At most 20
//     pages, 2,000 records and 8 MiB are read (history.go:162-175); past
//     that the chat says earlier messages are in the terminal
//     (history.go:183). Each message maps to the items the stream gave it,
//     under the same ids (history2.go:46-112): user prompts, assistant
//     text, reasoning and tool calls; no turn items, agents or shells.
//   - Typed, not posted: prompts (the adapter has no send action:
//     opencode2.yaml:239-247), the terminal's own commands (/new, /clear,
//     /connect and the others marked kind: terminal, opencode2.yaml:
//     282-317), and pictures, pasted one path at a time (images per_path,
//     opencode2.yaml:33). A prompt waits for the composer's footer row
//     (ready, opencode2.yaml:37; engine/ready.go:25-52).
//   - API calls: model and variant (POST /api/session/{id}/model), mode
//     (/agent), a server command (/command), compact, interrupt, answers
//     (/permission/{id}/reply, /form/{id}/reply), a start before the first
//     prompt (POST /api/session), provider keys, MCP connect and
//     disconnect; resume and show_session are relaunches with --session
//     (opencode2.yaml:193-268).
//   - Reads (opencode2.yaml:55-140): sessions (resume list), models and
//     variants (/api/model), providers, unconnected (/api/integration),
//     agents (modes, renewed by agents_rev), commands (renewed by
//     commands_rev), children (/api/session?parentID=, the agents section),
//     shells (/api/shell, the background section, every 10 s), mcp.
//   - Conversations: the chat follows the session of the last prompt the
//     terminal sent (session.inbox.enqueued in another top-level session
//     moves it, events2.go:372-388), or --session in a relaunch's argv
//     (events2.go:58-61). Child sessions are session.created with a
//     parentID (events2.go:349-370). Todos and diffs: 2.x has neither
//     event nor route in the adapter (no plan capability; changes are a
//     tool's files, parts2.go:103-108). Background shells: shell.created
//     and shell.exited or .deleted (shells2.go). Permissions:
//     permission.asked and .replied; questions and other forms:
//     form.created and .replied or .cancelled (needs2.go, fields2.go).
//
// Every event carries only fields that the mapper or the adapter's
// sources read and that appear in the recordings of OpenCode 2.2.5 under
// adapters/opencode/testdata/v2, with one exception quoted from the
// adapter's tests: a child session's session.created
// (sources2_test.go:164-166, built from the real children.json). The
// honesty test (honesty_opencode2_test.go) holds every event to them.
//
// Not written, for want of a real recording: a compaction
// (session.compaction.ended), a retry notice, a file-editing tool (so
// changes), a form with an address and a code (provider sign-in), and
// background subagents.
type OpenCode2 struct {
	// Mode, Provider and Model, when set, are the agent and model the
	// following turns run with, as after a switch.
	Mode, Provider, Model string

	main    string
	journal string
	shells  []string // background shells still running
	ask     *ask2
}

// ask2 is a turn waiting on a permission or a form.
type ask2 struct {
	user, msg, call string
	form            bool
}

// Name is the adapter's name.
func (p *OpenCode2) Name() string { return "opencode2" }

// Session is the id of the session the terminal shows now.
func (p *OpenCode2) Session() string { return p.main }

// Agent is the agent (Togo's mode) the turns run with: "build" by default
// (events.sse).
func (p *OpenCode2) Agent() string {
	if p.Mode != "" {
		return p.Mode
	}
	return "build"
}

func (p *OpenCode2) model() map[string]any {
	if p.Model != "" {
		return map[string]any{"id": p.Model, "providerID": p.Provider}
	}
	return map[string]any{"id": OpenCodeModel, "providerID": OpenCodeProvider}
}

// Running lists the background shells still running.
func (p *OpenCode2) Running() []string { return append([]string(nil), p.shells...) }

// emit writes one event. loc adds the folder the event happened in, as the
// real stream does for most events (events.sse; not for an execution's
// start and end, or an agent or model switch).
func (p *OpenCode2) emit(g *Gen, at time.Time, typ string, loc bool, data map[string]any) error {
	ev := map[string]any{"id": ocID(g, "evt"), "created": at.UnixMilli(), "type": typ, "data": data}
	if loc {
		ev["location"] = map[string]any{"directory": g.Cwd}
	}
	return g.Emit(p.journal, at, ev)
}

// Open writes the first session's creation.
func (p *OpenCode2) Open(g *Gen) error {
	p.journal = OpenCodeJournal(g.Home)
	g.L.Mains = []string{p.journal}
	_, err := p.NewSession(g, "", "")
	return err
}

// NewSession writes a session's creation and, for a top-level one, makes
// it the one the terminal shows. A child names its parent and agent
// (sources2_test.go:164-166). It returns the session's id.
func (p *OpenCode2) NewSession(g *Gen, parent, title string) (string, error) {
	id := ocID(g, "ses")
	at := g.Tick()
	if title == "" {
		title = "New session - " + at.Format("2006-01-02T15:04:05.000Z")
	}
	data := map[string]any{"sessionID": id, "title": title, "location": map[string]any{"directory": g.Cwd}}
	if parent != "" {
		data["parentID"], data["agent"] = parent, "general"
	}
	// The quoted child's event has no folder of its own (sources2_test.go).
	if err := p.emit(g, at, "session.created", parent == "", data); err != nil {
		return "", err
	}
	if parent == "" {
		p.main = id
	}
	return id, nil
}

// Turn writes one prompt and what the agent does with it.
func (p *OpenCode2) Turn(g *Gen, t Turn) error {
	switch {
	case t.Compact:
		return errors.New("sessiongen: no real recording of an OpenCode 2.x compaction")
	case t.Agents != nil && t.Agents.Background:
		return errors.New("sessiongen: no real recording of OpenCode 2.x background subagents")
	case t.Exit:
		return errors.New("sessiongen: an OpenCode 2.x exit is the terminal's, not an event")
	case p.ask != nil:
		return errors.New("sessiongen: a turn waits on a question")
	}
	if err := p.endShells(g); err != nil {
		return err
	}
	if t.New {
		if _, err := p.NewSession(g, "", ""); err != nil {
			return err
		}
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = "Step " + strconv.Itoa(g.L.Truth.Prompts+1) + ": " + g.Text(40)
	}
	user, err := p.Prompt(g, p.main, prompt)
	if err != nil {
		return err
	}
	if t.Error {
		// revert.sse: a turn the provider refused ends failed.
		return p.emit(g, g.Tick(), "session.execution.failed", false, map[string]any{"sessionID": p.main,
			"error": map[string]any{"type": "provider.quota", "message": "Quota exceeded", "status": 429}})
	}
	if err := p.emit(g, g.Tick(), "session.execution.started", false, map[string]any{"sessionID": p.main}); err != nil {
		return err
	}
	_ = user
	asst, err := p.step(g, p.main)
	if err != nil {
		return err
	}
	if err := p.thought(g, p.main, asst); err != nil {
		return err
	}
	for i := 0; i < t.Tools; i++ {
		if _, err := p.shell(g, p.main, asst, "echo step-"+strconv.Itoa(g.Serial()), t.Output); err != nil {
			return err
		}
	}
	for i := 0; i < t.Jobs; i++ {
		if err := p.background(g, "sleep "+strconv.Itoa(30+i)); err != nil {
			return err
		}
	}
	if t.Agents != nil {
		if err := p.children(g, t.Agents); err != nil {
			return err
		}
	}
	if t.Interrupt {
		return p.Interrupted(g, asst)
	}
	reply, err := p.step(g, p.main)
	if err != nil {
		return err
	}
	last, err := p.text(g, p.main, reply, max(t.Say, 1))
	if err != nil {
		return err
	}
	g.L.Truth.Last = last
	return p.emit(g, g.Tick(), "session.execution.succeeded", false, map[string]any{"sessionID": p.main})
}

// Prompt writes a user prompt the terminal sent to sess and returns its
// id, the inbox id the history gives the user message.
func (p *OpenCode2) Prompt(g *Gen, sess, text string) (string, error) {
	id := ocID(g, "msg")
	g.L.Truth.Prompts++
	if sess != p.main {
		p.main = sess // the terminal sent it there: it shows that session
	}
	return id, p.emit(g, g.Tick(), "session.inbox.enqueued", true, map[string]any{"inboxID": id, "sessionID": sess,
		"item": map[string]any{"type": "user", "payload": map[string]any{"text": text}}})
}

// step starts one step of the turn: an assistant message of its own.
func (p *OpenCode2) step(g *Gen, sess string) (string, error) {
	id := ocID(g, "msg")
	at := g.Tick()
	return id, p.emit(g, at, "session.step.started", true, map[string]any{"sessionID": sess, "agent": p.Agent(),
		"model": p.model(), "assistantMessageID": id})
}

// thought writes streamed reasoning.
func (p *OpenCode2) thought(g *Gen, sess, msg string) error {
	whole := g.Text(60)
	base := map[string]any{"sessionID": sess, "assistantMessageID": msg, "ordinal": 0}
	if err := p.emit(g, g.Tick(), "session.reasoning.started", true, base); err != nil {
		return err
	}
	if err := p.emit(g, g.Tick(), "session.reasoning.delta", true, with(base, "delta", whole)); err != nil {
		return err
	}
	return p.emit(g, g.Tick(), "session.reasoning.ended", true, with(base, "text", whole))
}

// text writes a reply streamed in pieces and returns its item id.
func (p *OpenCode2) text(g *Gen, sess, msg string, size int) (string, error) {
	whole := g.Text(size)
	base := map[string]any{"sessionID": sess, "assistantMessageID": msg, "ordinal": 0}
	if err := p.emit(g, g.Tick(), "session.text.started", true, base); err != nil {
		return "", err
	}
	for i := 0; i < len(whole); i += 64 {
		if err := p.emit(g, g.Tick(), "session.text.delta", true, with(base, "delta", whole[i:min(i+64, len(whole))])); err != nil {
			return "", err
		}
	}
	return msg + "-t0", p.emit(g, g.Tick(), "session.text.ended", true, with(base, "text", whole))
}

// shell writes one shell tool call, from its input to its result, and
// returns the call's id.
func (p *OpenCode2) shell(g *Gen, sess, msg, cmd string, size int) (string, error) {
	id := g.Hex(32)
	base := map[string]any{"sessionID": sess, "assistantMessageID": msg, "id": id}
	if err := p.emit(g, g.Tick(), "session.tool.input.started", true, with(base, "name", "shell")); err != nil {
		return "", err
	}
	if err := p.emit(g, g.Tick(), "session.tool.called", true, with(base, "input", map[string]any{"command": cmd})); err != nil {
		return "", err
	}
	return id, p.toolDone(g, base, g.Text(max(size, 1))+"\n")
}

func (p *OpenCode2) toolDone(g *Gen, base map[string]any, out string) error {
	done := with(base, "content", []any{map[string]any{"type": "text", "text": out}})
	done["metadata"] = map[string]any{"status": "completed", "truncated": false, "exit": 0}
	return p.emit(g, g.Tick(), "session.tool.success", true, done)
}

// background starts a shell that runs on after the turn (shells.sse).
func (p *OpenCode2) background(g *Gen, cmd string) error {
	id := "sh_" + fmt.Sprintf("%012x", g.Serial()) + g.Hex(14)
	at := g.Tick()
	p.shells = append(p.shells, id)
	g.L.Truth.Jobs = append(g.L.Truth.Jobs, id)
	return p.emit(g, at, "shell.created", true, map[string]any{"info": map[string]any{"id": id, "status": "running",
		"command": cmd, "metadata": map[string]any{"sessionID": p.main}, "time": map[string]any{"started": at.UnixMilli()}}})
}

// endShells writes the end of every running background shell
// (shells.sse: shell.exited).
func (p *OpenCode2) endShells(g *Gen) error {
	for _, id := range p.shells {
		if err := p.emit(g, g.Tick(), "shell.exited", true, map[string]any{"id": id}); err != nil {
			return err
		}
	}
	p.shells = nil
	return nil
}

// EndShells ends the running background shells now, as they finish while
// the agent is idle.
func (p *OpenCode2) EndShells(l *Layout) error {
	if err := p.endShells(l.g); err != nil {
		return err
	}
	return l.g.flush()
}

// children writes a batch of child sessions, each running its steps and
// ending, in a seeded order, inside the main turn.
func (p *OpenCode2) children(g *Gen, b *Batch) error {
	for _, i := range g.Shuffle(b.Size) {
		title := "Explore part " + strconv.Itoa(i+1) + " (@general subagent)"
		main := p.main
		child, err := p.NewSession(g, main, title)
		if err != nil {
			return err
		}
		if err := p.emit(g, g.Tick(), "session.execution.started", false, map[string]any{"sessionID": child}); err != nil {
			return err
		}
		msg, err := p.step(g, child)
		if err != nil {
			return err
		}
		for s := 0; s < b.Steps; s++ {
			if _, err := p.shell(g, child, msg, "ls part-"+strconv.Itoa(i+1), 40); err != nil {
				return err
			}
		}
		if _, err := p.text(g, child, msg, 30); err != nil {
			return err
		}
		if err := p.emit(g, g.Tick(), "session.execution.succeeded", false, map[string]any{"sessionID": child}); err != nil {
			return err
		}
		g.L.Truth.Agents = append(g.L.Truth.Agents, AgentTruth{Task: child, Prompt: title, File: p.journal, Exited: true, Settled: true})
	}
	return nil
}

// Interrupted writes the end of a turn the user stopped (events.sse: the
// step fails as aborted, the execution is interrupted).
func (p *OpenCode2) Interrupted(g *Gen, msg string) error {
	if err := p.emit(g, g.Tick(), "session.step.failed", true, map[string]any{"sessionID": p.main, "assistantMessageID": msg,
		"error": map[string]any{"type": "aborted", "message": "Step interrupted"}}); err != nil {
		return err
	}
	return p.emit(g, g.Tick(), "session.execution.interrupted", false, map[string]any{"sessionID": p.main})
}

func with(m map[string]any, k string, v any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for a, b := range m {
		out[a] = b
	}
	out[k] = v
	return out
}
