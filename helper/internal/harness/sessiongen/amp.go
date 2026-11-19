package sessiongen

import "fmt"

// Amp writes the session of Amp 0.0.17xx as Togo's Amp plugin forwards it
// (~/.config/amp/plugins/togo.ts, `togo hook amp <event>`). Amp keeps its
// threads on its own servers and writes nothing Togo reads.
//
// Events and the payload fields the adapter reads (hookstyle/amp.go
// ampPrep renames them: thread.id to session_id, message to prompt, tool
// to tool_name, input to tool_input, output to tool_response, toolUseID
// to tool_use_id, last to last_assistant_message):
//
//	session.start  thread.id (mapper.go:197)
//	agent.start    thread.id, message (mapper.go:202)
//	tool.result    thread.id, toolUseID, tool, input, status, output or
//	               error: one finished call, so it starts the call and ends
//	               it (mapper.go:243, Once); status error or cancelled
//	               fails it
//	agent.end      thread.id, status (done, error, cancelled), last: the
//	               turn's last reply (mapper.go:280)
//
// The plugin has no hook for permission, questions, subagents or the end
// of the Amp process (the hub reports that).
//
// Shapes: hookstyle/testdata/amp.jsonl, from the plugin API
// (`amp plugins show-docs`, 0.0.1794); the plugin was not seen live.
type Amp struct {
	thread string
	n      int
}

func (a *Amp) Name() string { return "amp" }

func (a *Amp) Open(g *Gen) error {
	a.thread = "T-" + uuid(g)
	g.L.Mains = append(g.L.Mains, "amp:"+a.thread)
	g.call(a.thread, "session.start", map[string]any{"thread": map[string]any{"id": a.thread}})
	return nil
}

func (a *Amp) Turn(g *Gen, t Turn) error {
	h := t.Hook
	if h == nil {
		h = &HookTurn{}
	}
	if t.Compact || t.Jobs > 0 || t.Agents != nil || h.Approve > 0 || h.Ask != nil {
		return fmt.Errorf("sessiongen: amp's plugin has no hook for compaction, jobs, subagents, permission or questions; its exit is the process's, which the hub reports")
	}
	if t.New {
		if err := a.Open(g); err != nil {
			return err
		}
	}
	prompt := t.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts+1, g.Text(40))
	}
	g.L.Truth.Prompts++
	a.n++
	th := map[string]any{"id": a.thread}
	g.call(a.thread, "agent.start", map[string]any{"thread": th, "message": prompt, "id": a.n})
	if h.Steer {
		g.L.Truth.Prompts++
		a.n++
		g.call(a.thread, "agent.start", map[string]any{"thread": th, "message": fmt.Sprintf("Step %d: %s", g.L.Truth.Prompts, g.Text(30)), "id": a.n})
	}
	for i := 0; i < t.Tools; i++ {
		id := "toolu_" + g.Hex(24)
		name, in := "Bash", map[string]any{"cmd": "echo " + g.Hex(4), "command": "echo " + g.Hex(4)}
		if i%3 == 1 {
			name, in = "Read", map[string]any{"path": g.Cwd + "/file" + g.Hex(2) + ".go"}
		}
		p := map[string]any{"thread": th, "toolUseID": id, "tool": name, "input": in, "status": "done",
			"output": map[string]any{"output": g.Text(max(h.Large, t.Output, 1)), "exitCode": 0}}
		if i < h.Fail {
			delete(p, "output")
			p["status"], p["error"] = "error", "file not found"
		}
		g.call(a.thread, "tool.result", p)
	}
	end := map[string]any{"thread": th, "message": prompt, "id": a.n, "status": "done", "last": g.Text(max(t.Say, 1))}
	switch {
	case t.Interrupt:
		end["status"], end["last"] = "cancelled", ""
	case t.Error:
		end["status"], end["last"] = "error", ""
	}
	if !h.NoStop {
		g.call(a.thread, "agent.end", end)
		g.ended(map[string]string{"done": "ok", "cancelled": "interrupted", "error": "failed"}[end["status"].(string)])
	}
	g.L.Truth.Exited = g.L.Truth.Exited || t.Exit
	return nil
}
