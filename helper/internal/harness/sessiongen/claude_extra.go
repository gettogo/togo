package sessiongen

import (
	"fmt"
	"path/filepath"
)

// ClaudeTurn is what a Claude Code turn can have besides the common script
// fields, keyed in Claude.Extra by the number of the turn's prompt (1 is
// the first). The records have the shapes of adapters/claude/testdata
// (transcript-real, transcript-cases, transcript-basic).
type ClaudeTurn struct {
	// Think writes a thinking block in its own assistant message before
	// the tools, as the real transcript has it.
	Think bool
	// Edits is the number of Edit calls; each changes one file.
	Edits int
	// Reads is the number of Read calls.
	Reads int
	// Todo is a TodoWrite call: its tasks, the first Done completed and
	// the next in progress.
	Todo []string
	Done int
}

// work writes the extras' calls after the prompt.
func (c *Claude) work(g *Gen, x ClaudeTurn) error {
	if x.Think {
		if err := c.entry(g, c.main, "", c.assistant(g, map[string]any{"type": "thinking", "thinking": "Think about " + g.Text(20)})); err != nil {
			return err
		}
	}
	call := func(name string, in map[string]any, out string) error {
		id := "toolu_" + g.Hex(24)
		if err := c.entry(g, c.main, "", c.assistant(g, toolUse(id, name, in))); err != nil {
			return err
		}
		return c.entry(g, c.main, "", c.result(id, out, nil))
	}
	for i := 0; i < x.Edits; i++ {
		p := filepath.Join(g.Cwd, fmt.Sprintf("e%d.go", i))
		if err := call("Edit", map[string]any{"file_path": p, "old_string": "x", "new_string": "y"}, "The file "+p+" has been updated successfully."); err != nil {
			return err
		}
	}
	for i := 0; i < x.Reads; i++ {
		p := filepath.Join(g.Cwd, fmt.Sprintf("r%d.go", i))
		if err := call("Read", map[string]any{"file_path": p}, g.Text(60)); err != nil {
			return err
		}
	}
	if len(x.Todo) > 0 {
		var todos []any
		for i, t := range x.Todo {
			st := "pending"
			switch {
			case i < x.Done:
				st = "completed"
			case i == x.Done:
				st = "in_progress"
			}
			todos = append(todos, map[string]any{"content": t, "status": st, "activeForm": t})
		}
		return call("TodoWrite", map[string]any{"todos": todos}, "Todos have been modified successfully.")
	}
	return nil
}
