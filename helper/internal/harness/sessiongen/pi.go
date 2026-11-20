package sessiongen

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Pi writes Pi's session files (JSONL v3): ~/.pi/agent/sessions/
// --<cwd with "/" as "->--/<time>_<id>.jsonl, one file per conversation
// in the folder (adapters/pi/pi.yaml, feeds.session). The shared pisession
// mapper reads them. Pi appends and closes per entry, so no process holds
// a file open between records.
//
// What the mapper reads, from adapters/pisession:
//   - the header "session" (pisession.go:94-100): id, cwd, timestamp; it
//     ends a running turn and sets harness_session;
//   - "model_change" (provider, modelId; :101, setModel :154-171) and
//     "thinking_level_change" (thinkingLevel; :103-110) set the status;
//   - "compaction" (tokensBefore; :111-118) and "session_info" (name;
//     :119-120) make a notice and the title;
//   - "message" (message.go:50-80) by role: user (content as a string or
//     blocks), assistant (text, thinking and toolCall blocks, provider,
//     model, stopReason, errorMessage; :82-134), toolResult (toolCallId,
//     isError; :163-176) and bashExecution (command, output, exitCode,
//     cancelled; :179-190).
//
// Pi writes no record of its own exit, no subagents and no background
// jobs (the adapter file says so), so Exit, Agents and Jobs are refused.
// Fields the mapper does not read (version, usage, api, the inner message
// timestamps) are left out.
type Pi struct {
	// Extra adds Pi-only records to a turn, keyed by the number of the
	// turn's prompt (1 is the first). Keyed by prompt so a script
	// appended in pieces keeps its meaning.
	Extra map[int]PiTurn

	id     string
	main   string
	parent string
	calls  int
	// model and effort are what the agent runs now; each reply names its
	// model, and a new conversation starts on both.
	model, effort string
	// files keeps each conversation's last entry id, to resume it.
	files map[string]string
}

// PiTurn is what a Pi turn can have besides the common script fields.
type PiTurn struct {
	// Model is "provider/id": a model_change before the prompt.
	Model string
	// Effort is a thinking level: a thinking_level_change before the prompt.
	Effort string
	// Edits is the number of edit tool calls before the reply; each
	// changes one file.
	Edits int
	// Bash is a command the user ran with "!" before the prompt.
	Bash string
	// Title writes a session_info with this name after the reply.
	Title string
	// Think writes a thinking block in the reply.
	Think bool
	// Resume, when above 0, returns to the Resume-th conversation file
	// (1 is the first) before the prompt, as `pi --session <id>` does:
	// Pi appends to that file and writes no new header.
	Resume int
	// Unfinished ends the turn after a tool call with no result and no
	// reply, as when Pi is killed at work.
	Unfinished bool
}

func (p *Pi) Name() string { return "pi" }

// folder is Pi's sessions folder for cwd: "-" + the path with "/" as "-"
// + "--", the spaces of the path kept.
func (p *Pi) folder(g *Gen) string {
	return filepath.Join(g.Home, ".pi", "agent", "sessions", "-"+strings.ReplaceAll(g.Cwd, "/", "-")+"--")
}

func (p *Pi) Open(g *Gen) error { return p.newFile(g) }

// newFile starts a conversation: the header, the model and the thinking
// level Pi records first.
func (p *Pi) newFile(g *Gen) error {
	at := g.Tick()
	p.id = fmt.Sprintf("%s-%s-4%s-8%s-%s", g.Hex(8), g.Hex(4), g.Hex(3), g.Hex(3), g.Hex(12))
	p.main = filepath.Join(p.folder(g), at.Format("2006-01-02T15-04-05-000Z")+"_"+p.id+".jsonl")
	p.parent = ""
	g.L.Mains = append(g.L.Mains, p.main)
	p.remember()
	if err := g.Emit(p.main, at, map[string]any{"type": "session", "id": p.id, "timestamp": stamp(at), "cwd": g.Cwd}); err != nil {
		return err
	}
	if p.model == "" {
		p.model, p.effort = "anthropic/claude-sonnet-4-5", "medium"
	}
	prov, mid, _ := strings.Cut(p.model, "/")
	if err := p.entry(g, map[string]any{"type": "model_change", "provider": prov, "modelId": mid}); err != nil {
		return err
	}
	return p.entry(g, map[string]any{"type": "thinking_level_change", "thinkingLevel": p.effort})
}

// entry adds id, parentId and timestamp and appends to the main file.
func (p *Pi) entry(g *Gen, e map[string]any) error {
	at := g.Tick()
	id := g.Hex(8)
	e["id"], e["timestamp"] = id, stamp(at)
	if p.parent == "" {
		e["parentId"] = nil
	} else {
		e["parentId"] = p.parent
	}
	p.parent = id
	p.remember()
	return g.Emit(p.main, at, e)
}

func (p *Pi) remember() {
	if p.files == nil {
		p.files = map[string]string{}
	}
	p.files[p.main] = p.parent
}

func (p *Pi) assistant(blocks []any, stop string) map[string]any {
	prov, mid, _ := strings.Cut(p.model, "/")
	return message(map[string]any{"role": "assistant", "content": blocks, "provider": prov, "model": mid, "stopReason": stop})
}

func (p *Pi) result(id, name, out string, failed bool) map[string]any {
	return message(map[string]any{"role": "toolResult", "toolCallId": id, "toolName": name, "content": textBlock(out), "isError": failed})
}

func (p *Pi) callID() string {
	p.calls++
	return fmt.Sprintf("call_%d", p.calls)
}

func (p *Pi) Turn(g *Gen, t Turn) error {
	if t.Agents != nil || t.Jobs > 0 || t.Exit {
		return fmt.Errorf("sessiongen: Pi has no subagents, background jobs or exit record")
	}
	if t.New {
		if err := p.newFile(g); err != nil {
			return err
		}
	}
	n := g.L.Truth.Prompts + 1
	x := p.Extra[n]
	if x.Resume > 0 {
		if x.Resume > len(g.L.Mains) {
			return fmt.Errorf("sessiongen: Pi has no conversation file %d to resume", x.Resume)
		}
		p.main = g.L.Mains[x.Resume-1]
		p.parent = p.files[p.main]
	}
	if x.Model != "" {
		p.model = x.Model
		prov, id, _ := strings.Cut(x.Model, "/")
		if err := p.entry(g, map[string]any{"type": "model_change", "provider": prov, "modelId": id}); err != nil {
			return err
		}
	}
	if x.Effort != "" {
		p.effort = x.Effort
		if err := p.entry(g, map[string]any{"type": "thinking_level_change", "thinkingLevel": x.Effort}); err != nil {
			return err
		}
	}
	if t.Compact {
		if err := p.entry(g, map[string]any{"type": "compaction", "tokensBefore": 120000}); err != nil {
			return err
		}
	}
	if x.Bash != "" {
		if err := p.entry(g, message(map[string]any{"role": "bashExecution", "command": x.Bash, "output": g.Text(30), "exitCode": 0, "cancelled": false})); err != nil {
			return err
		}
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", n, g.Text(40))
	}
	g.L.Truth.Prompts++
	// Pi writes a prompt as a string, or as text blocks when it has images.
	var content any = prompt
	if n%2 == 0 {
		content = textBlock(prompt)
	}
	if err := p.entry(g, message(map[string]any{"role": "user", "content": content})); err != nil {
		return err
	}
	for i := 0; i < t.Tools; i++ {
		id := p.callID()
		if err := p.entry(g, p.assistant([]any{toolCall(id, "read", map[string]any{"path": filepath.Join(g.Cwd, "f"+g.Hex(4)+".go")})}, "toolUse")); err != nil {
			return err
		}
		if err := p.entry(g, p.result(id, "read", g.Text(max(t.Output, 1)), false)); err != nil {
			return err
		}
	}
	for i := 0; i < x.Edits; i++ {
		id := p.callID()
		file := filepath.Join(g.Cwd, fmt.Sprintf("e%d_%d.go", n, i))
		if err := p.entry(g, p.assistant([]any{toolCall(id, "edit", map[string]any{"path": file, "oldText": "a", "newText": "b"})}, "toolUse")); err != nil {
			return err
		}
		if err := p.entry(g, p.result(id, "edit", "Edited "+filepath.Base(file), false)); err != nil {
			return err
		}
	}
	var err error
	switch {
	case x.Unfinished:
		err = p.entry(g, p.assistant([]any{toolCall(p.callID(), "bash", map[string]any{"command": "go test ./..."})}, "toolUse"))
	case t.Interrupt:
		err = p.entry(g, p.assistant([]any{toolCall(p.callID(), "bash", map[string]any{"command": "go test ./..."})}, "aborted"))
	case t.Error:
		a := p.assistant([]any{}, "error")
		a["message"].(map[string]any)["errorMessage"] = "401 Incorrect API key provided"
		err = p.entry(g, a)
	default:
		blocks := textBlock(g.Text(max(t.Say, 1)))
		if x.Think {
			blocks = append([]any{map[string]any{"type": "thinking", "thinking": g.Text(20)}}, blocks...)
		}
		err = p.entry(g, p.assistant(blocks, "stop"))
	}
	if err != nil {
		return err
	}
	if x.Title != "" {
		return p.entry(g, map[string]any{"type": "session_info", "name": x.Title})
	}
	return nil
}
