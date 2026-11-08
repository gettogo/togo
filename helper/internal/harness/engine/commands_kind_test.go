package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/auggie"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/codex"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/crush"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kilo"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kimi"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/opencode"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/pi"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// cmdWant is how one / command runs: kind, then role, action and opens.
type cmdWant struct{ kind, role, action, opens string }

// commandTable is A3 §1.4 for the adapters S2 owns: every listed command
// and how the palette runs it.
var commandTable = map[string]map[string]cmdWant{
	"aider": {
		"add": {kind: spec.CommandArgs}, "drop": {kind: spec.CommandArgs},
		"model":     {kind: spec.CommandPicker},
		"chat-mode": {kind: spec.CommandChoice, action: "set_mode"},
		"architect": {kind: spec.CommandArgs}, "ask": {kind: spec.CommandArgs},
		"diff":   {kind: spec.CommandRun, opens: spec.OpensChanges},
		"undo":   {kind: spec.CommandRun, action: "undo"},
		"tokens": {kind: spec.CommandTerminal},
		"clear":  {kind: spec.CommandRun, role: spec.RoleDestructive},
		"run":    {kind: spec.CommandArgs}, "test": {kind: spec.CommandArgs},
	},
	"auggie": {
		"model": {kind: spec.CommandPicker},
		"ask":   {kind: spec.CommandChoice, action: "set_mode"}, "plan": {kind: spec.CommandChoice, action: "set_mode"},
		"agents": {kind: spec.CommandTerminal}, "task": {kind: spec.CommandTerminal},
		"diffs":   {kind: spec.CommandRun, opens: spec.OpensChanges},
		"context": {kind: spec.CommandTerminal}, "stats": {kind: spec.CommandTerminal},
		"sessions": {kind: spec.CommandPicker},
	},
	"cline": {"resume": {kind: spec.CommandPicker}},
	"codex": {
		"model":   {kind: spec.CommandPicker},
		"compact": {kind: spec.CommandRun, action: "compact"},
		"review":  {kind: spec.CommandRun},
		"diff":    {kind: spec.CommandRun, opens: spec.OpensChanges},
		"status":  {kind: spec.CommandRun, opens: spec.OpensStatus},
	},
	"copilot": {
		"model": {kind: spec.CommandPicker}, "resume": {kind: spec.CommandPicker},
		"compact": {kind: spec.CommandRun, action: "compact"},
		"plan":    {kind: spec.CommandRun},
		"tasks":   {kind: spec.CommandTerminal},
		"diff":    {kind: spec.CommandRun, opens: spec.OpensChanges},
		"usage":   {kind: spec.CommandTerminal}, "context": {kind: spec.CommandTerminal},
	},
	"crush": {"sessions": {kind: spec.CommandPicker}},
	"droid": {
		"model":    {kind: spec.CommandTerminal},
		"compress": {kind: spec.CommandRun, action: "compact"},
		"resume":   {kind: spec.CommandPicker},
		"context":  {kind: spec.CommandTerminal}, "cost": {kind: spec.CommandTerminal},
		"droids": {kind: spec.CommandTerminal},
		"review": {kind: spec.CommandRun},
	},
	"goose": {
		"mode":    {kind: spec.CommandChoice, action: "set_mode"},
		"model":   {kind: spec.CommandArgs},
		"compact": {kind: spec.CommandRun, action: "compact"},
		"prompts": {kind: spec.CommandTerminal}, "skills": {kind: spec.CommandTerminal},
	},
	"kilo": {
		"models":   {kind: spec.CommandPicker},
		"sessions": {kind: spec.CommandPicker},
		"compact":  {kind: spec.CommandRun, action: "compact"},
	},
	"kimi": {
		"compact":  {kind: spec.CommandRun, action: "compact"},
		"model":    {kind: spec.CommandPicker},
		"sessions": {kind: spec.CommandPicker},
	},
	"omp": {
		"model":   {kind: spec.CommandPicker},
		"effort":  {kind: spec.CommandChoice},
		"compact": {kind: spec.CommandRun, action: "compact"},
	},
	"opencode": {
		"models":   {kind: spec.CommandPicker},
		"sessions": {kind: spec.CommandPicker},
		"compact":  {kind: spec.CommandRun, action: "compact"},
	},
	"pi": {
		"model": {kind: spec.CommandPicker}, "resume": {kind: spec.CommandPicker},
		"compact": {kind: spec.CommandRun, action: "compact"},
		"new":     {kind: spec.CommandRun, role: spec.RoleDestructive},
		"name":    {kind: spec.CommandArgs},
		"session": {kind: spec.CommandRun, opens: spec.OpensStatus},
		"tree":    {kind: spec.CommandTerminal}, "fork": {kind: spec.CommandTerminal}, "settings": {kind: spec.CommandTerminal},
	},
	"vibe": {
		"compact": {kind: spec.CommandRun, action: "compact"},
		"config":  {kind: spec.CommandTerminal},
		"resume":  {kind: spec.CommandPicker},
	},
}

// Every command of every non-Claude adapter runs the way A3 §1.4 says,
// and every adapter file still compiles as the engine loads it.
func TestCommandKindsPerAdapter(t *testing.T) {
	files := harness.AdapterFiles()
	for name, want := range commandTable {
		b, ok := files[name]
		if !ok {
			t.Errorf("%s: not registered", name)
			continue
		}
		a, err := compileAdapter(b, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got := map[string]bool{}
		for _, c := range a.Commands.Static {
			got[c.Name] = true
			w, ok := want[c.Name]
			if !ok {
				t.Errorf("%s /%s: not in the table", name, c.Name)
				continue
			}
			have := cmdWant{kind: a.CommandKind(c), role: c.Role, action: c.Action, opens: c.Opens}
			if have != w {
				t.Errorf("%s /%s: %+v, want %+v", name, c.Name, have, w)
			}
		}
		for c := range want {
			if !got[c] {
				t.Errorf("%s /%s: missing from the file", name, c)
			}
		}
	}
}

// No adapter keeps a confirm-only compact screen: compact is a status
// action, a command's action, or both; and every screen a command or the
// status block names still exists.
func TestCompactIsInline(t *testing.T) {
	files := harness.AdapterFiles()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "claude" {
			continue // S1 moves Claude's
		}
		a, err := compileAdapter(files[name], name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		for id, sc := range a.Screens {
			if sc.Primitive == harness.PrimConfirm && len(sc.Fields) == 0 && strings.Contains(sc.Submit, "compact") {
				t.Errorf("%s: screen %s is a page for one yes", name, id)
			}
			if sc.Primitive == harness.PrimStatus && sc.Section == harness.SectionStatus && name == "codex" {
				t.Errorf("codex: usage screen %s still listed", id)
			}
		}
	}
}
