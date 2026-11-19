package sessiongen

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Codex writes what the Codex app-server sends its clients, as a journal
// of one JSON-RPC message per line. Codex keeps no file the helper reads:
// the helper reads the app-server as a second client of the terminal's.
//
// How the helper finds and connects to it:
//   - At launch the session launcher asks the engine for the agent's
//     arguments (engine/endpoint.go:124 EndpointArgs). The engine first
//     runs `codex --version` against the adapter's range (codex.yaml:26,
//     ">=0.144 <0.168"); outside it the agent starts with no endpoint
//     (endpoint.go:133) and the session has its terminal only. Inside it
//     the engine picks a socket, TOGO_HOME/run/harness/<session>.sock
//     (endpoint.go:166-184), starts `codex app-server --listen
//     unix://SOCK` in the session folder in its own process group
//     (codex.yaml:29, engine/server.go:41-104), waits up to 8 s for it to
//     answer initialize (server.go:23, 140-174), records it under run/
//     for a helper restart (engine/recovery.go:190, 212: the server
//     process is adopted again by pid and birth time, the socket by its
//     file identity), and gives the terminal client `--remote
//     unix://SOCK` (codex.yaml:30).
//   - Transport: a WebSocket over the unix socket, one JSON-RPC 2.0
//     message per text frame, at most 16 MiB (source/rpc/transport.go:21,
//     38). No auth: the socket folder is 0700 (endpoint.go:172).
//   - Handshake on every connection: initialize with clientInfo
//     {name: togo} and capabilities {experimentalApi: true}, then the
//     initialized notification (source/rpc/runner.go:93-106).
//
// What it reads, and when (the feed, codex.yaml:37-51):
//   - On connect: account/rateLimits/read once (rpc/attach.go:44), then
//     every 2 s thread/loaded/list (attach.go:74-84, runner.go:60), and
//     thread/resume on each loaded thread it has not joined
//     (attach.go:98). The resume result carries the thread (id, parent,
//     nickname, role, status, every turn with its items), the model,
//     effort, approval policy and folder; the mapper replays it as
//     history (adapters/codex/appserver.go:196-288). A top-level thread is
//     the main one if it is the first, is active, or is fresher while the
//     main one is idle (appserver.go:237); a top-level thread that starts
//     a turn becomes main (appserver.go:366: the desk's /new or /resume).
//     A thread with a parent is a subagent, drawn in its own chat.
//   - Then the notifications listed in codex.yaml:45-49: turn/started and
//     turn/completed (working, turn rows), item/started and
//     item/completed (prompts, replies, reasoning, commands, file changes,
//     collab and subagent activity: items.go:196, agents.go:68,
//     activities.go:13), the three deltas (live.go:17, 57), plan and diff
//     (live.go:95, 131), token usage and rate limits (live.go:171, 212),
//     thread status (live.go:237), settings (model, effort, approval
//     policy: live.go:291-325), compacted, rerouted, error (live.go:332-
//     379), and serverRequest/resolved (live.go:594). Server requests
//     (approvals, permissions, questions, MCP elicitations) are kept by
//     the runner per connection until answered (runner.go:267, 286) and
//     shown as questions (live.go:381-592).
//   - Sources on a separate pooled connection (runner.go:110-171):
//     model/list for the model and effort pickers, thread/list for the
//     resume picker (codex.yaml:56-72).
//   - A dropped connection: the feed emits rpc/closed (runner.go:213),
//     which ends working and drops open questions (appserver.go:112-115,
//     live.go:623), and reconnects with backoff from 0.5 s to 10 s
//     (runner.go:24-27); a new connection resumes every loaded thread
//     again, whose history replaces what was read.
//
// Actions (codex.yaml:99-146): interrupt is turn/interrupt with the main
// thread and turn; compact is thread/compact/start; the mode picker is
// config/value/write of approval_policy (Codex's config file, written by
// Codex, applying to new threads); answers reply to the server request
// (rpc_reply, adapters/codex/answer.go). Model, effort and resume restart
// the terminal client (`codex resume <thread> -m ...`). Prompts and /
// commands are typed into the terminal: the helper never starts a turn.
//
// The hook feed (codex.yaml:52-55, adapters/codex/hooks.go) keeps state
// from Codex's hooks, and draws a chat only while no app-server feed is
// live (hooks.go:14-19, 41-58): it is the fallback for a Codex started
// without the app-server.
//
// As a second client the helper sees what the terminal's client does: the
// terminal starts the turns, its own approvals reach the helper as the
// same server requests, the first answer from either wins and the server
// says so with serverRequest/resolved.
//
// What this profile writes: thread/started, turn/started and
// turn/completed, item/started and item/completed (userMessage,
// reasoning, agentMessage, commandExecution, fileChange,
// collabAgentToolCall), the three deltas, plan, diff, token usage, rate
// limits, thread status, settings, error, and the fileChange approval and
// request_user_input server requests, with the fields of
// adapters/codex/testdata (codex_msgs.go names the line of each).
// serverRequest/resolved is not written: the server sends it when a
// request is answered (enginetest.CodexServer does).
//
// Not written, for want of a real recording: thread/compacted,
// contextCompaction items, model/rerouted, a commandExecution approval
// request (only its params are quoted in appserver_test.go), and any
// answer to thread/list (no recording holds one).
type Codex struct {
	// Model and Effort are what the threads run with; Policy is the
	// approval policy new threads take.
	Model, Effort, Policy string
	// Approval makes the next turn's file change wait on an approval
	// request; Ask makes it ask a question first. Each applies once.
	Approval, Ask bool

	journal string
	main    string // the thread the terminal shows
	cwd     string
	request int // the next server request id
}

// CodexJournal is where the Codex profile writes the journal under home:
// test data, not a file Codex keeps.
func CodexJournal(home string) string {
	return filepath.Join(home, ".togo-test", "codex-appserver.jsonl")
}

// CodexModel and CodexEffort are the model and effort of the recordings
// (appserver-0167.jsonl:1).
const (
	CodexModel  = "gpt-6.1-sol"
	CodexEffort = "low"
	CodexPolicy = "on-request"
)

func (p *Codex) Name() string { return "codex" }

// Thread is the id of the thread the terminal shows now.
func (p *Codex) Thread() string { return p.main }

// Journal is the journal's path.
func (p *Codex) Journal() string { return p.journal }

func (p *Codex) settings() (string, string) {
	m, e := p.Model, p.Effort
	if m == "" {
		m = CodexModel
	}
	if e == "" {
		e = CodexEffort
	}
	return m, e
}

func (p *Codex) emit(g *Gen, at time.Time, m CodexMsg) error { return g.Emit(p.journal, at, m) }

// Open writes the terminal's first thread and the account's limits.
func (p *Codex) Open(g *Gen) error {
	p.journal, p.cwd = CodexJournal(g.Home), g.Cwd
	g.L.Mains = []string{p.journal}
	return p.newThread(g)
}

func (p *Codex) newThread(g *Gen) error {
	at := g.Tick()
	p.main = codexUUID(g)
	m, e := p.settings()
	return p.emit(g, at, CodexThreadStarted(CodexThread(p.main, "", "", "", p.cwd, m, e, at)))
}

// Turn writes one prompt and what Codex does with it.
func (p *Codex) Turn(g *Gen, t Turn) error {
	if t.Compact {
		return errors.New("sessiongen: no Codex compaction is recorded")
	}
	if t.Jobs > 0 {
		return errors.New("sessiongen: Codex has no background shells")
	}
	if t.New {
		if err := p.newThread(g); err != nil {
			return err
		}
	}
	th := p.main
	turn := codexUUID(g)
	start := g.Tick()
	steps := []error{
		p.emit(g, start, CodexTurnStarted(th, CodexTurn(turn, "inProgress", start, nil, nil))),
		p.emit(g, start, CodexStatus(th, "active")),
	}
	prompt := t.Prompt
	if prompt == "" || prompt == Continue {
		prompt = "Please " + g.Text(40)
	}
	g.L.Truth.Prompts++
	user := CodexUserMessage(codexUUID(g), prompt)
	at := g.Tick()
	steps = append(steps, p.emit(g, at, CodexItemStarted(th, turn, user, at)), p.emit(g, at, CodexItemCompleted(th, turn, user, at)))
	for _, err := range steps {
		if err != nil {
			return err
		}
	}
	if err := p.think(g, th, turn); err != nil {
		return err
	}
	for i := 0; i < t.Tools; i++ {
		if err := p.command(g, th, turn, i, t.Output, t.Interrupt && i == t.Tools-1); err != nil {
			return err
		}
	}
	if t.Interrupt {
		return p.end(g, th, turn, start, "interrupted", nil)
	}
	if err := p.edit(g, th, turn); err != nil {
		return err
	}
	if t.Agents != nil {
		if err := p.spawn(g, th, turn, t.Agents); err != nil {
			return err
		}
	}
	if t.Error {
		at := g.Tick()
		msg := "Your access token could not be refreshed. Please log out and sign in again."
		if err := p.emit(g, at, CodexError(th, turn, msg, "unauthorized", false)); err != nil {
			return err
		}
		return p.end(g, th, turn, start, "failed", CodexFailure(msg, "unauthorized"))
	}
	reply := "msg_" + g.Hex(24)
	text := g.Text(max(t.Say, 2))
	at = g.Tick()
	half := len(text) / 2
	for _, m := range []CodexMsg{
		CodexItemStarted(th, turn, CodexAgentMessage(reply, "", "final_answer"), at),
		CodexDelta("item/agentMessage/delta", th, turn, reply, text[:half]),
		CodexDelta("item/agentMessage/delta", th, turn, reply, text[half:]),
		CodexItemCompleted(th, turn, CodexAgentMessage(reply, text, "final_answer"), g.Tick()),
		CodexTokens(th, turn, 20000*g.L.Truth.Prompts, 9000+t.Say, 256000),
		CodexLimitsUpdated(CodexRateLimits(10+g.L.Truth.Prompts%80, 3, at.Add(5*time.Hour))),
	} {
		if err := p.emit(g, at, m); err != nil {
			return err
		}
	}
	g.L.Truth.Last = reply
	if err := p.end(g, th, turn, start, "completed", nil); err != nil {
		return err
	}
	if t.Exit {
		g.L.Truth.Exited = true
		return p.emit(g, g.Tick(), CodexStatus(th, "notLoaded"))
	}
	return nil
}

func (p *Codex) end(g *Gen, th, turn string, start time.Time, status string, failure map[string]any) error {
	at := g.Tick()
	if err := p.emit(g, at, CodexStatus(th, "idle")); err != nil {
		return err
	}
	return p.emit(g, at, CodexTurnCompleted(th, CodexTurn(turn, status, start, &at, failure)))
}

// think writes a reasoning item, streamed, and a plan.
func (p *Codex) think(g *Gen, th, turn string) error {
	id := "rs_" + g.Hex(24)
	at := g.Tick()
	sum := "Checking " + g.Text(12)
	for _, m := range []CodexMsg{
		CodexItemStarted(th, turn, CodexReasoning(id, nil), at),
		CodexDelta("item/reasoning/summaryTextDelta", th, turn, id, sum),
		CodexItemCompleted(th, turn, CodexReasoning(id, []string{sum}), g.Tick()),
		CodexPlan(th, turn, []string{"Read the code", "Make the change", "Run the tests"}, 1),
	} {
		if err := p.emit(g, at, m); err != nil {
			return err
		}
	}
	return nil
}

// command writes one shell command with its output; an interrupted one
// ends as the user's abort (appserver-attach.jsonl:24).
func (p *Codex) command(g *Gen, th, turn string, n, size int, aborted bool) error {
	id := "exec-" + codexUUID(g)
	cmd := fmt.Sprintf("go test ./pkg%d/...", n)
	at := g.Tick()
	if err := p.emit(g, at, CodexItemStarted(th, turn, CodexCommand(id, cmd, p.cwd, "inProgress", nil, nil, nil), at)); err != nil {
		return err
	}
	out, code, status := g.Text(max(size, 1))+"\n", 0, "completed"
	if aborted {
		out, code, status = "command aborted by user", -1, "failed"
	} else if err := p.emit(g, at, CodexDelta("item/commandExecution/outputDelta", th, turn, id, out)); err != nil {
		return err
	}
	d := int64(900)
	return p.emit(g, at, CodexItemCompleted(th, turn, CodexCommand(id, cmd, p.cwd, status, &out, &code, &d), g.Tick()))
}

// edit writes a file change, behind an approval or a question when the
// profile is told to, and the turn's diff.
func (p *Codex) edit(g *Gen, th, turn string) error {
	id := "exec-" + codexUUID(g)
	name := fmt.Sprintf("file%d.go", g.Serial())
	path := filepath.Join(p.cwd, name)
	diff := "@@ -1,3 +1,3 @@\n func Add(a, b int) int {\n-\treturn a - b\n+\treturn a + b\n }\n"
	at := g.Tick()
	var msgs []CodexMsg
	if p.Ask {
		p.Ask = false
		msgs = append(msgs, CodexAsk(p.next(), th, turn, "call_"+g.Hex(8), "scope", "Scope", "Fix only Add or the whole file?", "Only Add", "Whole file"))
	}
	msgs = append(msgs, CodexItemStarted(th, turn, CodexFileChange(id, "inProgress", path, "update", diff), at))
	if p.Approval {
		p.Approval = false
		msgs = append(msgs, CodexFileApproval(p.next(), th, turn, id, at))
	}
	msgs = append(msgs, CodexItemCompleted(th, turn, CodexFileChange(id, "completed", path, "update", diff), g.Tick()),
		CodexDiff(th, turn, "diff --git a/"+name+" b/"+name+"\n--- a/"+name+"\n+++ b/"+name+"\n"+diff))
	for _, m := range msgs {
		if err := p.emit(g, at, m); err != nil {
			return err
		}
	}
	return nil
}

func (p *Codex) next() int {
	id := p.request
	p.request++
	return id
}

// spawn starts subagent threads as Codex 0.167 does: the spawnAgent
// collab call (appserver-turn.jsonl:27), the child's own thread with its
// parent, and subAgentActivity records in the parent's turn when the
// child starts and completes (appserver-agent-0167-2.jsonl). The child's
// turn runs its steps and replies; Unreported children of a background
// batch never complete.
func (p *Codex) spawn(g *Gen, th, turn string, b *Batch) error {
	for i := 0; i < b.Size; i++ {
		child := codexUUID(g)
		name := fmt.Sprintf("check_package_%d", i+1)
		task := fmt.Sprintf("Check package %d\nfor the same bug", i+1)
		at := g.Tick()
		m, e := p.settings()
		unreported := b.Background && i >= b.Size-b.Unreported
		act := "activity-" + g.Hex(8)
		msgs := []CodexMsg{
			CodexThreadStarted(CodexThread(child, th, fmt.Sprintf("Scout%d", i+1), "explorer", p.cwd, m, e, at)),
			CodexItemCompleted(th, turn, CodexSpawn("call_"+g.Hex(12), th, child, task, "completed", "running"), at),
			CodexItemStarted(th, turn, CodexActivity(act, "started", child, "/root/"+name, at, nil), at),
			CodexItemCompleted(th, turn, CodexActivity(act, "started", child, "/root/"+name, at, &at), at),
		}
		ct := codexUUID(g)
		cs := g.Tick()
		msgs = append(msgs, CodexTurnStarted(child, CodexTurn(ct, "inProgress", cs, nil, nil)))
		for s := 0; s < b.Steps; s++ {
			id := "exec-" + codexUUID(g)
			out, code, d := g.Text(40)+"\n", 0, int64(300)
			msgs = append(msgs, CodexItemStarted(child, ct, CodexCommand(id, "ls", p.cwd, "inProgress", nil, nil, nil), g.Tick()),
				CodexItemCompleted(child, ct, CodexCommand(id, "ls", p.cwd, "completed", &out, &code, &d), g.Tick()))
		}
		truth := AgentTruth{Task: child, Prompt: strings.SplitN(task, "\n", 2)[0], File: p.journal}
		if !unreported {
			reply := "msg_" + g.Hex(24)
			ce := g.Tick()
			done := "activity-" + g.Hex(8)
			msgs = append(msgs, CodexItemCompleted(child, ct, CodexAgentMessage(reply, "No other copies.", "final_answer"), ce),
				CodexTurnCompleted(child, CodexTurn(ct, "completed", cs, &ce, nil)),
				CodexItemStarted(th, turn, CodexActivity(done, "completed", child, "/root/"+name, ce, nil), ce),
				CodexItemCompleted(th, turn, CodexActivity(done, "completed", child, "/root/"+name, ce, &ce), ce))
			truth.Settled, truth.Exited = true, true
		}
		for _, msg := range msgs {
			if err := p.emit(g, at, msg); err != nil {
				return err
			}
		}
		g.L.Truth.Agents = append(g.L.Truth.Agents, truth)
	}
	return nil
}
