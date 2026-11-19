package sessiongen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// Kimi writes Kimi Code's wire log, protocol 1.5: ~/.kimi-code/sessions/
// wd_<folder name>_<sha256(cwd)[:12]>/session_<id>/agents/main/wire.jsonl
// (adapters/kimi/kimi.yaml, feeds.wire). A /new starts another session
// folder with a new log. A relaunch with --continue carries on in the
// same log with no new header (adapters/kimi/interact_test.go).
//
// What the mapper reads, from adapters/kimi:
//   - metadata (protocol_version) starts a log; profile.bind and
//     config.update set model and thinking effort (wire.go:117-118);
//     permission.set_mode and plan_mode.enter/exit set the mode;
//   - turn.prompt (promptId, input, origin.kind, turnId; wire.go:200-225)
//     opens a turn and writes the user item; agent.turn.started marks it
//     working; turn.ended ends it (reason completed or failed, error);
//     turn.cancel ends it interrupted; turn.steer with origin
//     background_task reports a background task;
//   - context.append_loop_event (loop.go): step.begin, content.part
//     (think, text), tool.call, tool.result, step.end;
//   - interaction.request and interaction.resolved (interact.go): a
//     question or an approval, and its answer;
//   - full_compaction.complete: a notice.
//
// Not generated, for want of a real recording: a subagent (the Agent
// tool's call and its notice are quoted only in the adapter's tests,
// hand-shortened), task.started, an interrupt (turn.cancel and the
// turn.ended reason "cancelled" are in no recorded log), leaving plan
// mode (plan_mode.exit) and the agent's own exit (none is written).
// Records the mapper does not read (context.append_message, agent.*
// records, token counts, usage) are left out, and so are fields it does
// not read.
type Kimi struct {
	// Extra adds Kimi-only records to a turn, keyed by the number of the
	// turn's prompt (1 is the first).
	Extra map[int]KimiTurn

	session string
	main    string
	turn    int // the log's own turn counter, from 0
	turns   map[string]int
	tasks   int
	model   string
	effort  string
}

// KimiTurn is what a Kimi turn can have besides the common script fields.
type KimiTurn struct {
	// Model and Effort write a config.update before the prompt.
	Model, Effort string
	// Mode writes permission.set_mode before the prompt ("manual" is the
	// ask mode, "yolo", "auto").
	Mode string
	// Plan writes plan_mode.enter before the prompt. (plan_mode.exit has
	// no real recording; leaving plan mode is not generated.)
	Plan bool
	// Resume, when above 0, returns to the Resume-th log (1 is the
	// first) before the prompt, as `kimi --session <id>` does: Kimi
	// carries on in that log with no new header.
	Resume int
	// Edits is the number of Write calls before the reply.
	Edits int
	// Todos writes a TodoList call with these statuses ("done",
	// "in_progress", "pending").
	Todos []string
	// Detached starts this many background jobs that are never reported
	// finished, as when Kimi is still at work on them.
	Detached int
	// Approval raises a Bash approval in the turn.
	Approval *KimiApproval
	// Question raises a question in the turn.
	Question *KimiQuestion
	// Unfinished ends the turn after a tool call with no result, as when
	// Kimi is killed at work. An approval or question left Open does too.
	Unfinished bool
}

// KimiApproval is an approval of a Bash command.
type KimiApproval struct {
	Command string
	// Decision is "approved" or "rejected"; Scope "session" for
	// "Approve for this session"; Feedback is the reason given with a
	// rejection.
	Decision, Scope, Feedback string
	// Open leaves it unanswered: the turn waits for the user.
	Open bool
}

// KimiQuestion is an AskUserQuestion with options.
type KimiQuestion struct {
	Question string
	Options  []string
	Answer   string
	Multi    bool
	// Open leaves it unanswered.
	Open bool
}

func (k *Kimi) Name() string { return "kimi" }

func (k *Kimi) folder(g *Gen) string {
	sum := sha256.Sum256([]byte(g.Cwd))
	return filepath.Join(g.Home, ".kimi-code", "sessions", "wd_"+filepath.Base(g.Cwd)+"_"+hex.EncodeToString(sum[:])[:12])
}

func (k *Kimi) Open(g *Gen) error { return k.newLog(g) }

func (k *Kimi) newLog(g *Gen) error {
	if k.turns == nil {
		k.turns = map[string]int{}
	}
	if k.main != "" {
		k.turns[k.main] = k.turn
	}
	k.session = fmt.Sprintf("%s-%s-4%s-8%s-%s", g.Hex(8), g.Hex(4), g.Hex(3), g.Hex(3), g.Hex(12))
	k.main = filepath.Join(k.folder(g), "session_"+k.session, "agents", "main", "wire.jsonl")
	g.L.Mains = append(g.L.Mains, k.main)
	k.turn = 0
	if k.model == "" {
		k.model, k.effort = "qwen5090/qwen3.8-27b", "high"
	}
	at := g.Tick()
	if err := g.Emit(k.main, at, map[string]any{"type": "metadata", "protocol_version": "1.5"}); err != nil {
		return err
	}
	if err := k.rec(g, map[string]any{"type": "profile.bind", "modelAlias": k.model, "thinkingEffort": k.effort}); err != nil {
		return err
	}
	return k.rec(g, map[string]any{"type": "permission.set_mode", "mode": "manual"})
}

// rec stamps a record with the log's clock and appends it.
func (k *Kimi) rec(g *Gen, r map[string]any) error {
	at := g.Tick()
	r["time"] = at.UnixMilli()
	return g.Emit(k.main, at, r)
}

func (k *Kimi) loop(g *Gen, e map[string]any) error {
	return k.rec(g, map[string]any{"type": "context.append_loop_event", "event": e})
}

// step writes one model step: begin, optional thinking and text, the
// calls, and the end. calls are written by the caller between.
func (k *Kimi) step(g *Gen, think, say string, body func(step string) error, finish string) error {
	step := uuid(g)
	if err := k.loop(g, map[string]any{"type": "step.begin"}); err != nil {
		return err
	}
	if think != "" {
		if err := k.loop(g, map[string]any{"type": "content.part", "stepUuid": step, "part": map[string]any{"type": "think", "think": think}}); err != nil {
			return err
		}
	}
	if say != "" {
		if err := k.loop(g, map[string]any{"type": "content.part", "stepUuid": step, "part": map[string]any{"type": "text", "text": say}}); err != nil {
			return err
		}
	}
	if body != nil {
		if err := body(step); err != nil {
			return err
		}
	}
	return k.loop(g, map[string]any{"type": "step.end", "finishReason": finish})
}

func toolCallID(g *Gen) string { return g.Hex(8) + g.Hex(8) + g.Hex(8) + g.Hex(8) }

func (k *Kimi) call(g *Gen, step, id, name string, args, display map[string]any) error {
	e := map[string]any{"type": "tool.call", "stepUuid": step, "toolCallId": id, "name": name, "args": args}
	if display != nil {
		e["display"] = display
	}
	return k.loop(g, e)
}

func (k *Kimi) result(g *Gen, id, out string, failed bool) error {
	r := map[string]any{"output": out}
	if failed {
		r["isError"] = true
	}
	return k.loop(g, map[string]any{"type": "tool.result", "toolCallId": id, "result": r})
}

func (k *Kimi) Turn(g *Gen, t Turn) error {
	if t.Agents != nil {
		return fmt.Errorf("sessiongen: the Kimi profile writes no subagents: no real recording of one")
	}
	if t.Interrupt || t.Exit {
		return fmt.Errorf("sessiongen: the Kimi profile writes no interrupt or exit: no real recording of either")
	}
	if t.New {
		if err := k.newLog(g); err != nil {
			return err
		}
	}
	n := g.L.Truth.Prompts + 1
	x := k.Extra[n]
	if x.Resume > 0 {
		if x.Resume > len(g.L.Mains) {
			return fmt.Errorf("sessiongen: Kimi has no log %d to resume", x.Resume)
		}
		k.turns[k.main] = k.turn
		k.main = g.L.Mains[x.Resume-1]
		k.turn = k.turns[k.main]
	}
	if x.Model != "" || x.Effort != "" {
		r := map[string]any{"type": "config.update"}
		if x.Model != "" {
			k.model = x.Model
			r["modelAlias"] = x.Model
		}
		if x.Effort != "" {
			k.effort = x.Effort
			r["thinkingEffort"] = x.Effort
		}
		if err := k.rec(g, r); err != nil {
			return err
		}
	}
	if x.Mode != "" {
		if err := k.rec(g, map[string]any{"type": "permission.set_mode", "mode": x.Mode}); err != nil {
			return err
		}
	}
	if x.Plan {
		if err := k.rec(g, map[string]any{"type": "plan_mode.enter", "id": "plan-" + g.Hex(6)}); err != nil {
			return err
		}
	}
	if t.Compact {
		if err := k.rec(g, map[string]any{"type": "full_compaction.complete"}); err != nil {
			return err
		}
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", n, g.Text(40))
	}
	g.L.Truth.Prompts++
	if err := k.rec(g, map[string]any{"type": "turn.prompt", "turnId": k.turn, "promptId": "msg_" + strings.ToUpper(g.Hex(26)),
		"input": []any{map[string]any{"type": "text", "text": prompt}}, "origin": map[string]any{"kind": "user"}}); err != nil {
		return err
	}
	k.turn++
	if err := k.rec(g, map[string]any{"type": "agent.turn.started", "turnId": k.turn - 1, "kind": "event"}); err != nil {
		return err
	}
	for i := 0; i < t.Tools; i++ {
		id := toolCallID(g)
		err := k.step(g, "", "", func(step string) error {
			if err := k.call(g, step, id, "Read", map[string]any{"path": filepath.Join(g.Cwd, "f"+g.Hex(4)+".go")},
				map[string]any{"kind": "file_io", "operation": "read", "path": filepath.Join(g.Cwd, "f"+g.Hex(4)+".go")}); err != nil {
				return err
			}
			return k.result(g, id, g.Text(max(t.Output, 1)), false)
		}, "tool_use")
		if err != nil {
			return err
		}
	}
	for i := 0; i < x.Edits; i++ {
		id, file := toolCallID(g), filepath.Join(g.Cwd, fmt.Sprintf("e%d_%d.go", n, i))
		err := k.step(g, "", "", func(step string) error {
			if err := k.call(g, step, id, "Write", map[string]any{"path": file, "content": g.Text(20)},
				map[string]any{"kind": "file_io", "operation": "write", "path": file}); err != nil {
				return err
			}
			return k.result(g, id, "Wrote "+filepath.Base(file), false)
		}, "tool_use")
		if err != nil {
			return err
		}
	}
	if len(x.Todos) > 0 {
		var todos []any
		for i, s := range x.Todos {
			todos = append(todos, map[string]any{"title": fmt.Sprintf("Task %d of step %d", i+1, n), "status": s})
		}
		id := toolCallID(g)
		err := k.step(g, "", "", func(step string) error {
			if err := k.call(g, step, id, "TodoList", map[string]any{"todos": todos}, map[string]any{"kind": "todo_list"}); err != nil {
				return err
			}
			return k.result(g, id, "Todo list updated", false)
		}, "tool_use")
		if err != nil {
			return err
		}
	}
	var open bool
	if q := x.Question; q != nil {
		o, err := k.question(g, *q)
		if err != nil {
			return err
		}
		open = open || o
	}
	if a := x.Approval; a != nil {
		o, err := k.approval(g, *a)
		if err != nil {
			return err
		}
		open = open || o
	}
	for i := 0; i < x.Detached; i++ {
		if _, err := k.job(g); err != nil {
			return err
		}
		g.L.Truth.Jobs = g.L.Truth.Jobs[:len(g.L.Truth.Jobs)-1] // not settled
	}
	var settle []string
	for i := 0; i < t.Jobs; i++ {
		task, err := k.job(g)
		if err != nil {
			return err
		}
		settle = append(settle, task)
	}
	if open {
		return nil // the turn waits for the user
	}
	switch {
	case x.Unfinished:
		id := toolCallID(g)
		return k.step(g, "", "", func(step string) error {
			return k.call(g, step, id, "Bash", map[string]any{"command": "go test ./..."}, map[string]any{"kind": "command", "command": "go test ./..."})
		}, "tool_use")
	case t.Error:
		return k.rec(g, map[string]any{"type": "turn.ended", "turnId": k.turn - 1, "reason": "failed",
			"error": map[string]any{"code": "provider.auth_error", "message": "401 Incorrect API key provided", "name": "AuthenticationError"}})
	}
	think := ""
	if n%5 == 0 {
		think = g.Text(20)
	}
	if err := k.step(g, think, g.Text(max(t.Say, 1)), nil, "end_turn"); err != nil {
		return err
	}
	for _, task := range settle {
		if err := k.rec(g, map[string]any{"type": "turn.steer", "input": []any{map[string]any{"type": "text", "text": task + " finished"}},
			"origin": map[string]any{"kind": "background_task", "taskId": task, "status": "completed"}}); err != nil {
			return err
		}
	}
	return k.rec(g, map[string]any{"type": "turn.ended", "turnId": k.turn - 1, "reason": "completed"})
}

// job starts a background shell command: Kimi answers the call with the
// task's id and status running, and reports its end with a steer. The id
// is returned; the caller writes the steer.
func (k *Kimi) job(g *Gen) (string, error) {
	k.tasks++
	task := "bash-" + g.Hex(8)
	id := toolCallID(g)
	cmd := "sleep 15; echo " + g.Hex(6)
	g.L.Truth.Jobs = append(g.L.Truth.Jobs, task)
	err := k.step(g, "", "", func(step string) error {
		if err := k.call(g, step, id, "Bash", map[string]any{"command": cmd, "run_in_background": true},
			map[string]any{"kind": "command", "command": cmd}); err != nil {
			return err
		}
		return k.result(g, id, "task_id: "+task+"\ndescription: "+cmd+"\nstatus: running", false)
	}, "tool_use")
	return task, err
}

// question raises an AskUserQuestion; it reports whether it is left open.
func (k *Kimi) question(g *Gen, q KimiQuestion) (bool, error) {
	id := toolCallID(g)
	var opts []any
	for _, o := range q.Options {
		opts = append(opts, map[string]any{"label": o, "description": "Pick " + o})
	}
	qs := map[string]any{"question": q.Question, "options": opts}
	if q.Multi {
		qs["multiSelect"] = true
	}
	ask := "question_" + uuid(g)
	err := k.step(g, "", "", func(step string) error {
		if err := k.call(g, step, id, "AskUserQuestion", map[string]any{"questions": []any{map[string]any{"question": q.Question, "options": opts}}}, nil); err != nil {
			return err
		}
		if err := k.rec(g, map[string]any{"type": "interaction.request", "id": ask, "kind": "question", "toolCallId": id,
			"request": map[string]any{"questions": []any{qs}}}); err != nil {
			return err
		}
		if q.Open {
			return nil
		}
		if err := k.rec(g, map[string]any{"type": "interaction.resolved", "id": ask,
			"response": map[string]any{"answers": map[string]any{q.Question: q.Answer}}}); err != nil {
			return err
		}
		return k.result(g, id, `{"answers":{"`+q.Question+`":"`+q.Answer+`"}}`, false)
	}, "tool_use")
	return q.Open, err
}

// approval raises the approval of a Bash command; it reports whether it
// is left open.
func (k *Kimi) approval(g *Gen, a KimiApproval) (bool, error) {
	id, req := toolCallID(g), "approval_"+uuid(g)
	disp := map[string]any{"kind": "command", "command": a.Command}
	reqDisp := map[string]any{"kind": "command"}
	err := k.step(g, "", "", func(step string) error {
		if err := k.call(g, step, id, "Bash", map[string]any{"command": a.Command}, disp); err != nil {
			return err
		}
		if err := k.rec(g, map[string]any{"type": "interaction.request", "id": req, "kind": "approval", "toolCallId": id,
			"request": map[string]any{"toolName": "Bash", "action": "Running: " + a.Command, "display": reqDisp}}); err != nil {
			return err
		}
		if a.Open {
			return nil
		}
		resp := map[string]any{"decision": a.Decision}
		if a.Scope != "" {
			resp["scope"] = a.Scope
		}
		if a.Feedback != "" {
			resp["feedback"] = a.Feedback
		}
		if err := k.rec(g, map[string]any{"type": "interaction.resolved", "id": req, "response": resp}); err != nil {
			return err
		}
		if a.Decision == "rejected" {
			return k.result(g, id, "Tool \"Bash\" was not run because the user rejected the approval request.", true)
		}
		return k.result(g, id, g.Text(10), false)
	}, "tool_use")
	return a.Open, err
}
