package spec

import (
	"slices"
	"strings"
)

// Command kinds: how the app runs a / command.
const (
	// CommandChoice: a short list of known values, picked in place.
	CommandChoice = "choice"
	// CommandPicker: a long or searchable list, or a form, pushed.
	CommandPicker = "picker"
	// CommandArgs: the command needs words; it goes to the composer.
	CommandArgs = "args"
	// CommandRun: no or optional arguments; it runs at once.
	CommandRun = "run"
	// CommandTerminal: it opens a TUI dialog or prints only to the
	// screen, so the app switches to the terminal.
	CommandTerminal = "terminal"
)

// CommandKinds lists every command kind.
var CommandKinds = []string{CommandChoice, CommandPicker, CommandArgs, CommandRun, CommandTerminal}

// Native homes a command may open instead of typing, besides a screen.
const (
	OpensStatus  = "status"
	OpensChanges = "changes"
)

// RoleDestructive is the only command role.
const RoleDestructive = "destructive"

// EffectRelaunch is an action that restarts the harness.
const EffectRelaunch = "relaunch"

// MaxChoices is the longest static list drawn as a choice menu.
const MaxChoices = 8

// CommandKind is the command's kind: the declared one, else derived. A
// screen with one choice field or a short static list is a choice; any
// other screen is a picker; a hint <x> needs args; anything else runs.
func (a *Adapter) CommandKind(c StaticCommand) string {
	if c.Kind != "" {
		return c.Kind
	}
	if c.Action != "" || c.Opens != "" {
		return CommandRun
	}
	if c.Screen != "" {
		if a.ChoiceScreen(c.Screen) {
			return CommandChoice
		}
		return CommandPicker
	}
	return HintKind(c.Hint)
}

// ModeAction reports whether name is the action that sets the mode: a
// command running it with kind choice is the Mode row.
func (a *Adapter) ModeAction(name string) bool {
	return name != "" && a.Modes != nil && a.Modes.Set == name
}

// HintKind derives a kind from an argument hint alone: "<x>" is a required
// argument, "[x]" or none runs.
func HintKind(hint string) string {
	if strings.HasPrefix(strings.TrimSpace(hint), "<") {
		return CommandArgs
	}
	return CommandRun
}

// ChoiceScreen reports whether a screen is a short list of known values:
// a settings screen with exactly one choice field, or a picker over a
// static source of at most MaxChoices entries.
func (a *Adapter) ChoiceScreen(name string) bool {
	sc, ok := a.Screens[name]
	if !ok {
		return false
	}
	switch sc.Primitive {
	case "settings":
		return len(sc.Fields) == 1 && sc.Fields[0].Kind == "choice" && len(sc.Fields[0].Options) > 0
	case "picker":
		src, ok := a.Sources[sc.Source]
		if !ok {
			return false
		}
		list, ok := src.Static.([]any)
		return ok && len(list) > 0 && len(list) <= MaxChoices
	}
	return false
}

// ActionEffect is what running the action costs: EffectRelaunch when it
// declares it or when any route it may take relaunches the harness, since
// the app must treat a possible restart as a restart.
func (a *Action) ActionEffect() string {
	if a.Effect != "" {
		return a.Effect
	}
	if a.Relaunch != nil {
		return EffectRelaunch
	}
	for _, br := range a.Branch {
		if br.Relaunch != nil {
			return EffectRelaunch
		}
	}
	return ""
}

// validateCommands checks the static commands' keys and the status block's
// actions.
func (a *Adapter) validateCommands(bad func(string, ...any)) {
	if st := a.Status; st != nil {
		for _, act := range st.Actions {
			if !a.hasAction(act) {
				bad("status.actions: unknown action %q", act)
			}
		}
	}
	for _, name := range sortedKeys(a.Actions) {
		if e := a.Actions[name].Effect; e != "" && e != EffectRelaunch {
			bad("actions.%s: effect %q is not relaunch", name, e)
		}
	}
	if a.Harness.Stderr != "" && a.Harness.Stderr != StderrMerge {
		bad("harness.stderr: %q is not merge", a.Harness.Stderr)
	}
	c := a.Commands
	if c == nil {
		return
	}
	seen := map[string]bool{}
	for _, cmd := range c.Static {
		where := "commands.static." + cmd.Name
		if cmd.Name == "" {
			bad("commands.static: a command needs a name")
		}
		if seen[cmd.Name] {
			bad("%s: listed twice", where)
		}
		seen[cmd.Name] = true
		if cmd.Kind != "" && !slices.Contains(CommandKinds, cmd.Kind) {
			bad("%s: kind %q is not one of %s", where, cmd.Kind, strings.Join(CommandKinds, ", "))
		}
		if cmd.Role != "" && cmd.Role != RoleDestructive {
			bad("%s: role %q is not destructive", where, cmd.Role)
		}
		if cmd.Screen != "" {
			if _, ok := a.Screens[cmd.Screen]; !ok {
				bad("%s: unknown screen %q", where, cmd.Screen)
			}
		}
		if cmd.Action != "" && !a.hasAction(cmd.Action) {
			bad("%s: unknown action %q", where, cmd.Action)
		}
		if o := cmd.Opens; o != "" && o != OpensStatus && o != OpensChanges {
			if _, ok := a.Screens[o]; !ok {
				bad("%s: opens %q is not status, changes or a screen", where, o)
			}
		}
		if n := countSet(cmd.Screen != "", cmd.Action != "", cmd.Opens != ""); n > 1 {
			bad("%s: screen, action and opens are exclusive", where)
		}
		switch cmd.Kind {
		case CommandChoice:
			if cmd.Screen == "" && !a.ModeAction(cmd.Action) {
				bad("%s: kind choice needs a screen or the modes action", where)
			}
		case CommandPicker:
			if cmd.Screen == "" {
				bad("%s: kind picker needs a screen", where)
			}
		case CommandArgs, CommandTerminal:
			if cmd.Screen != "" || cmd.Action != "" || cmd.Opens != "" {
				bad("%s: kind %s types the command; drop screen, action and opens", where, cmd.Kind)
			}
		}
	}
}

func countSet(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}
