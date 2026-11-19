package sessiongen

import (
	"fmt"
	"path/filepath"
	"strings"
)

// OMPTurn is what an omp turn can have besides the common script fields,
// keyed in OMP.Extra by the number of the turn's prompt (1 is the first).
// Every record it writes has the shape of a real one in adapters/omp/
// testdata (session.jsonl, todo-init.jsonl, ask.jsonl).
type OMPTurn struct {
	// Model is "provider/id": a model_change before the prompt.
	Model string
	// Effort is a thinking level: a thinking_level_change before it.
	Effort string
	// Bash is a command the user ran with "!" before the prompt.
	Bash string
	// Fork starts a new conversation file whose header names the one it
	// came from (omp's /fork), before the prompt.
	Fork bool
	// Think writes a thinking block in the reply.
	Think bool
	// Edits is the number of edit tool calls before the reply; each
	// changes one file.
	Edits int
	// Picture is the name of an image file in the folder that a read call
	// opens.
	Picture string
	// Search is the number of grep calls before the reply.
	Search int
	// Todo are calls of omp's todo tool, in order.
	Todo []OMPTodo
	// Ask is a question the agent puts to the user.
	Ask *OMPAsk
}

// OMPTodo is one call that sets the plan: Op "init" is omp's todo tool
// with Phases (its first task starts in progress); Op "write" is the
// todo_write tool, a flat list in which Done tasks are completed and the
// first of the rest is in progress.
type OMPTodo struct {
	Op     string
	Phases []OMPPhase
	Tasks  []string
	Done   int
}

// OMPPhase is a named group of tasks.
type OMPPhase struct {
	Name  string
	Tasks []string
}

// OMPAsk is a call of the ask tool with one question.
type OMPAsk struct {
	ID, Header, Question string
	Options              []string
	// Answer is the label chosen. Cancelled ends the call with the
	// user's Escape. With neither, the call is left open: the dialog is
	// still on screen.
	Answer    string
	Cancelled bool
}

// before writes what the extras put before the prompt.
func (o *OMP) before(g *Gen, x OMPTurn) error {
	if x.Fork {
		if err := o.fork(g); err != nil {
			return err
		}
	}
	if x.Model != "" {
		o.model = x.Model
		if err := o.entry(g, map[string]any{"type": "model_change", "model": x.Model}); err != nil {
			return err
		}
	}
	if x.Effort != "" {
		if err := o.entry(g, map[string]any{"type": "thinking_level_change", "thinkingLevel": x.Effort, "configured": x.Effort}); err != nil {
			return err
		}
	}
	if x.Bash != "" {
		return o.entry(g, message(map[string]any{"role": "bashExecution", "command": x.Bash, "output": "ok " + g.Hex(3),
			"exitCode": 0, "cancelled": false}))
	}
	return nil
}

// fork starts a conversation file whose header names the one before it.
func (o *OMP) fork(g *Gen) error {
	o.forkOf = o.id
	err := o.newFile(g)
	o.forkOf = ""
	return err
}

// work writes the extras' calls after the prompt; open reports an ask left
// without a result.
func (o *OMP) work(g *Gen, x OMPTurn) (open bool, err error) {
	call := func(name string, args map[string]any, out string, details map[string]any) error {
		id := "call_" + g.Hex(8)
		if err := o.entry(g, o.assistant(g, []any{toolCall(id, name, args)}, "toolUse")); err != nil {
			return err
		}
		return o.entry(g, toolResult(id, out, details))
	}
	for i := 0; i < x.Edits; i++ {
		rel := fmt.Sprintf("e%d.go", i)
		if err := call("edit", map[string]any{"path": filepath.Join(g.Cwd, rel), "oldText": "a", "newText": "b"}, "Edited "+rel, nil); err != nil {
			return false, err
		}
	}
	if x.Picture != "" {
		if err := call("read", map[string]any{"path": filepath.Join(g.Cwd, x.Picture)}, "Read "+x.Picture, nil); err != nil {
			return false, err
		}
	}
	for i := 0; i < x.Search; i++ {
		if err := call("grep", map[string]any{"pattern": "Parse\\("}, "main.go:12\nlexer.go:40", nil); err != nil {
			return false, err
		}
	}
	for _, td := range x.Todo {
		if err := o.todo(g, td, call); err != nil {
			return false, err
		}
	}
	if a := x.Ask; a != nil {
		return o.ask(g, *a)
	}
	return false, nil
}

func (o *OMP) todo(g *Gen, td OMPTodo, call func(string, map[string]any, string, map[string]any) error) error {
	switch td.Op {
	case "init":
		var list, phases []any
		for pi, p := range td.Phases {
			list = append(list, map[string]any{"phase": p.Name, "items": p.Tasks})
			var tasks []any
			for ti, t := range p.Tasks {
				st := "pending"
				if pi == 0 && ti == 0 {
					st = "in_progress"
				}
				tasks = append(tasks, map[string]any{"content": t, "status": st})
			}
			phases = append(phases, map[string]any{"name": p.Name, "tasks": tasks})
		}
		return call("todo", map[string]any{"op": "init", "i": "Setting up the plan", "list": list}, "Plan set", map[string]any{"phases": phases})
	case "write":
		var todos []any
		for i, t := range td.Tasks {
			st := "pending"
			switch {
			case i < td.Done:
				st = "completed"
			case i == td.Done:
				st = "in_progress"
			}
			todos = append(todos, map[string]any{"content": t, "status": st})
		}
		return call("todo_write", map[string]any{"todos": todos}, fmt.Sprintf("%d todos", len(todos)), nil)
	}
	return fmt.Errorf("sessiongen: omp todo op %q", td.Op)
}

func (o *OMP) ask(g *Gen, a OMPAsk) (open bool, err error) {
	id := "toolu_" + g.Hex(12)
	var opts []any
	for _, l := range a.Options {
		opts = append(opts, map[string]any{"label": l, "description": "About " + strings.ToLower(l) + "."})
	}
	q := map[string]any{"id": a.ID, "header": a.Header, "question": a.Question, "options": opts}
	if err := o.entry(g, o.assistant(g, []any{toolCall(id, "ask", map[string]any{"questions": []any{q}})}, "toolUse")); err != nil {
		return false, err
	}
	switch {
	case a.Cancelled:
		m := toolResult(id, "Ask tool was cancelled by the user", map[string]any{})
		m["message"].(map[string]any)["isError"] = true
		return false, o.entry(g, m)
	case a.Answer != "":
		return false, o.entry(g, toolResult(id, "User selected: "+a.Answer, map[string]any{"selectedOptions": []any{a.Answer}}))
	}
	o.open = &ompOpenAsk{id: id, question: a.Question, options: a.Options}
	return true, nil
}

// ompOpenAsk is an ask call whose dialog is still on screen.
type ompOpenAsk struct {
	id, question string
	options      []string
}

// Answer writes what omp writes when the open ask is answered at its
// dialog: the call's result, and the agent's reply that ends the turn.
// An empty label is the user's Escape.
func (o *OMP) Answer(l *Layout, label string) error {
	if o.open == nil {
		return fmt.Errorf("sessiongen: no ask is open")
	}
	a, g := o.open, l.g
	o.open = nil
	m := toolResult(a.id, "User selected: "+label, map[string]any{"selectedOptions": []any{label}})
	if label == "" {
		m = toolResult(a.id, "Ask tool was cancelled by the user", map[string]any{})
		m["message"].(map[string]any)["isError"] = true
	}
	if err := o.entry(g, m); err != nil {
		return err
	}
	if err := o.entry(g, o.assistant(g, textBlock("Noted. "+g.Text(30)), "stop")); err != nil {
		return err
	}
	g.L.Truth.Last = o.parent
	return g.flush()
}

// Change writes what omp writes when the user switches the model or the
// thinking level in its own prompt (/switch, /effort): the change record
// alone, with no prompt. Either may be empty.
func (o *OMP) Change(l *Layout, model, effort string) error {
	x := OMPTurn{Model: model, Effort: effort}
	if err := o.before(l.g, x); err != nil {
		return err
	}
	return l.g.flush()
}
