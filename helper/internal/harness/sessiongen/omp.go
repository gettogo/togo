package sessiongen

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// OMP writes omp's session files: ~/.omp/agent/sessions/<folder>/
// <time>_<id>.jsonl, a folder of subagent files next to it, and the
// terminal record ~/.omp/agent/terminal-sessions/<tty> naming the file the
// terminal shows. Shapes follow adapters/omp/testdata.
type OMP struct {
	// TTY is the pane's terminal without /dev/ ("ttys042"); empty writes
	// no terminal record.
	TTY string

	id     string // the current conversation's id
	main   string // its file
	parent string // id of the last record in the main file
	jobs   int    // omp numbers background jobs bg_1, bg_2, ...
	tasks  int
	later  []report // background agents reported at the next turn
}

type report struct {
	name  string
	truth int
}

func (o *OMP) Name() string { return "omp" }

// sessionsDir is omp's sessions folder for a folder under home: "-" and
// the path from home with "/" as "-".
func (o *OMP) sessionsDir(g *Gen) string {
	rel, _ := filepath.Rel(g.Home, g.Cwd)
	name := "-" + strings.ReplaceAll(filepath.ToSlash(rel), "/", "-")
	return filepath.Join(g.Home, ".omp", "agent", "sessions", name)
}

func (o *OMP) Open(g *Gen) error { return o.newFile(g) }

// newFile starts a conversation: a title line, then the session header,
// and moves the terminal record to it.
func (o *OMP) newFile(g *Gen) error {
	at := g.Tick()
	o.id = g.Hex(10)
	o.main = filepath.Join(o.sessionsDir(g), at.Format("2006-01-02T15-04-05-000Z")+"_"+o.id+".jsonl")
	g.L.Mains = append(g.L.Mains, o.main)
	o.parent = ""
	if err := g.Emit(o.main, at, map[string]any{"type": "title", "title": g.Text(20)}); err != nil {
		return err
	}
	if err := g.Emit(o.main, at, map[string]any{"type": "session", "id": o.id, "timestamp": stamp(at)}); err != nil {
		return err
	}
	if err := o.entry(g, map[string]any{"type": "model_change", "model": "llama.cpp/qwen3.8-27b"}); err != nil {
		return err
	}
	if o.TTY == "" {
		return nil
	}
	g.L.Terminal = filepath.Join(g.Home, ".omp", "agent", "terminal-sessions", strings.ReplaceAll(o.TTY, "/", "-"))
	return g.Replace(g.L.Terminal, at, g.Cwd+"\n"+o.main+"\n")
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// entry adds an id, parentId and timestamp and appends to the main file.
func (o *OMP) entry(g *Gen, e map[string]any) error {
	at := g.Tick()
	id := g.Hex(8)
	e["id"], e["timestamp"] = id, stamp(at)
	if o.parent != "" {
		e["parentId"] = o.parent
	} else {
		e["parentId"] = nil
	}
	o.parent = id
	return g.Emit(o.main, at, e)
}

func message(m map[string]any) map[string]any { return map[string]any{"type": "message", "message": m} }

func textBlock(s string) []any { return []any{map[string]any{"type": "text", "text": s}} }

func (o *OMP) assistant(g *Gen, content []any, stop string) map[string]any {
	return message(map[string]any{"role": "assistant", "content": content, "provider": "llama.cpp", "model": "qwen3.8-27b",
		"usage":      map[string]any{"input": 1200 + g.Intn(800), "output": 40, "cacheRead": 800, "cost": map[string]any{"total": 0.001}},
		"stopReason": stop})
}

func toolCall(id, name string, args map[string]any) map[string]any {
	return map[string]any{"type": "toolCall", "id": id, "name": name, "arguments": args}
}

func toolResult(id string, out string, details map[string]any) map[string]any {
	m := map[string]any{"role": "toolResult", "toolCallId": id, "content": textBlock(out), "isError": false}
	if details != nil {
		m["details"] = details
	}
	return message(m)
}

func (o *OMP) Turn(g *Gen, t Turn) error {
	if err := o.reportLater(g); err != nil {
		return err
	}
	if t.New {
		if err := o.newFile(g); err != nil {
			return err
		}
	}
	if t.Compact {
		if err := o.entry(g, map[string]any{"type": "compaction", "method": "snapcompact", "tokensBefore": 120000, "tokensAfter": 31000}); err != nil {
			return err
		}
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts+1, g.Text(40))
	}
	g.L.Truth.Prompts++
	if err := o.entry(g, message(map[string]any{"role": "user", "content": textBlock(prompt)})); err != nil {
		return err
	}
	for i := 0; i < t.Tools; i++ {
		id := "call_" + g.Hex(8)
		if err := o.entry(g, o.assistant(g, []any{toolCall(id, "read", map[string]any{"path": filepath.Join(g.Cwd, "f"+g.Hex(4)+".go")})}, "toolUse")); err != nil {
			return err
		}
		if err := o.entry(g, toolResult(id, g.Text(max(t.Output, 1)), nil)); err != nil {
			return err
		}
	}
	for i := 0; i < t.Jobs; i++ {
		if err := o.job(g); err != nil {
			return err
		}
	}
	if t.Agents != nil {
		if err := o.batch(g, *t.Agents); err != nil {
			return err
		}
	}
	var err error
	switch {
	case t.Interrupt:
		err = o.entry(g, o.assistant(g, []any{toolCall("call_"+g.Hex(8), "bash", map[string]any{"command": "go test ./..."})}, "aborted"))
		g.L.Truth.Last = o.parent + "-interrupted"
	case t.Error:
		a := o.assistant(g, []any{}, "error")
		a["message"].(map[string]any)["errorStatus"] = 401
		a["message"].(map[string]any)["errorMessage"] = "401 Incorrect API key provided"
		err = o.entry(g, a)
		g.L.Truth.Last = o.parent + "-error"
	default:
		err = o.entry(g, o.assistant(g, textBlock(g.Text(max(t.Say, 1))), "stop"))
		g.L.Truth.Last = o.parent
	}
	if err != nil {
		return err
	}
	if t.Exit {
		g.L.Truth.Exited = true
		return o.entry(g, map[string]any{"type": "custom", "customType": "session_exit", "data": map[string]any{"kind": "signal"}})
	}
	return nil
}

// job starts a background shell job and settles it, as omp 18.20 records
// "bash" with async: true.
func (o *OMP) job(g *Gen) error {
	o.jobs++
	id := fmt.Sprintf("bg_%d", o.jobs)
	call := "call_" + g.Hex(8)
	cmd := "sleep 15; echo " + g.Hex(6)
	g.L.Truth.Jobs = append(g.L.Truth.Jobs, id)
	if err := o.entry(g, o.assistant(g, []any{toolCall(call, "bash", map[string]any{"command": cmd, "async": true})}, "toolUse")); err != nil {
		return err
	}
	if err := o.entry(g, toolResult(call, "Backgrounded as job "+id, map[string]any{"async": map[string]any{"state": "running", "jobId": id, "type": "bash"}})); err != nil {
		return err
	}
	return o.entry(g, map[string]any{"type": "custom_message", "customType": "async-result",
		"details": map[string]any{"jobs": []any{map[string]any{"jobId": id, "type": "bash", "status": "completed", "label": cmd}}}})
}

// batch writes a task call, its subagents' files in a seeded order, and
// the reports: the call's results for a foreground batch, an async-result
// per agent for a background one.
func (o *OMP) batch(g *Gen, b Batch) error {
	call := "call_" + g.Hex(8)
	names := make([]string, b.Size)
	var tasks []any
	for i := range names {
		o.tasks++
		names[i] = fmt.Sprintf("Task%dx%s", o.tasks, g.Hex(4))
		tasks = append(tasks, map[string]any{"name": names[i], "task": "Complete assignment thoroughly:\n\n# Target\n" + names[i] + " " + g.Text(30)})
	}
	if err := o.entry(g, o.assistant(g, []any{toolCall(call, "task", map[string]any{"tasks": tasks})}, "toolUse")); err != nil {
		return err
	}
	if b.Background {
		if err := o.entry(g, toolResult(call, fmt.Sprintf("Started %d agents", b.Size),
			map[string]any{"async": map[string]any{"state": "running", "jobId": names[0], "type": "task"}})); err != nil {
			return err
		}
		if err := o.entry(g, o.assistant(g, textBlock("The agents are running."), "stop")); err != nil {
			return err
		}
	}
	order := g.Shuffle(b.Size)
	base := len(g.L.Truth.Agents)
	for _, i := range order {
		prompt := tasks[i].(map[string]any)["task"].(string)
		if err := o.subagent(g, names[i], prompt, b.Steps, i >= b.Size-b.Unreported); err != nil {
			return err
		}
	}
	if !b.Background {
		var results []any
		for _, n := range names {
			results = append(results, map[string]any{"id": n, "exitCode": 0, "output": n + " finished"})
		}
		for k := range order {
			g.L.Truth.Agents[base+k].Settled = true
		}
		return o.entry(g, toolResult(call, fmt.Sprintf("%d tasks finished", b.Size), map[string]any{"results": results}))
	}
	for _, i := range g.Shuffle(b.Size) {
		if i < b.Size-b.Unreported {
			o.later = append(o.later, report{names[i], base + slot(order, i)})
		}
	}
	if b.ReportLater {
		return nil
	}
	return o.reportLater(g)
}

// reportLater writes omp's async-result for each background agent not yet
// reported, then the agent's reply to them.
func (o *OMP) reportLater(g *Gen) error {
	if len(o.later) == 0 {
		return nil
	}
	for _, r := range o.later {
		if err := o.entry(g, map[string]any{"type": "custom_message", "customType": "async-result",
			"details": map[string]any{"jobs": []any{map[string]any{"jobId": r.name, "agentId": r.name, "type": "task", "status": "completed", "label": r.name}}}}); err != nil {
			return err
		}
		g.L.Truth.Agents[r.truth].Settled = true
	}
	o.later = nil
	if err := o.entry(g, o.assistant(g, textBlock("The agents are back."), "stop")); err != nil {
		return err
	}
	g.L.Truth.Last = o.parent
	return nil
}

// slot is the position of v in order.
func slot(order []int, v int) int {
	for k, x := range order {
		if x == v {
			return k
		}
	}
	return -1
}

// subagent writes one subagent's whole file: title, header naming the
// main file, the assignment, its steps, its reply and its exit (omp 18.21
// ends every subagent file with session_exit). An open agent's file stops
// after its steps: it is still at work.
func (o *OMP) subagent(g *Gen, name, prompt string, steps int, open bool) error {
	path := filepath.Join(strings.TrimSuffix(o.main, ".jsonl"), name+".jsonl")
	g.L.Subagents = append(g.L.Subagents, path)
	g.L.Truth.Agents = append(g.L.Truth.Agents, AgentTruth{Task: name, Prompt: prompt, File: path, Exited: !open})
	at := g.Tick()
	if err := g.Emit(path, at, map[string]any{"type": "title", "title": ""}); err != nil {
		return err
	}
	if err := g.Emit(path, at, map[string]any{"type": "session", "id": g.Hex(8) + "-" + g.Hex(4), "timestamp": stamp(at), "parentSession": o.main}); err != nil {
		return err
	}
	parent := ""
	sub := func(e map[string]any) error {
		at := g.Tick()
		id := g.Hex(8)
		e["id"], e["timestamp"] = id, stamp(at)
		if parent == "" {
			e["parentId"] = nil
		} else {
			e["parentId"] = parent
		}
		parent = id
		return g.Emit(path, at, e)
	}
	if err := sub(message(map[string]any{"role": "user", "content": prompt})); err != nil {
		return err
	}
	for i := 0; i < steps; i++ {
		id := "sub_" + g.Hex(8)
		if err := sub(o.assistant(g, []any{toolCall(id, "grep", map[string]any{"pattern": "Parse\\("})}, "toolUse")); err != nil {
			return err
		}
		if err := sub(toolResult(id, "main.go:12\nlexer.go:40", nil)); err != nil {
			return err
		}
	}
	if open {
		return nil // still at work
	}
	if err := sub(o.assistant(g, textBlock(name+" finished"), "stop")); err != nil {
		return err
	}
	return sub(map[string]any{"type": "custom", "customType": "session_exit", "data": map[string]any{"kind": "normal"}})
}
