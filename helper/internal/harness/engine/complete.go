package engine

import (
	"context"
	"slices"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// completeCommands lists the / menu: the adapter's own commands first, then
// the harness's, each with how the app runs it.
func (e *Engine) completeCommands(ctx context.Context, s *sess, query string, limit int) []harness.Completion {
	c := s.ad.Commands
	if c == nil {
		return nil
	}
	var all []harness.Completion
	for _, sc := range c.Static {
		all = append(all, e.staticCompletion(ctx, s, sc))
	}
	if c.Source != "" {
		all = append(all, e.sourceCompletions(ctx, s, c)...)
	}
	q := strings.ToLower(strings.TrimPrefix(query, "/"))
	var out []harness.Completion
	seen := map[string]bool{}
	for _, pass := range []func(string) bool{
		func(n string) bool { return strings.HasPrefix(n, q) },
		func(n string) bool { return !strings.HasPrefix(n, q) && strings.Contains(n, q) },
	} {
		for _, cm := range all {
			n := strings.ToLower(strings.TrimPrefix(cm.Title, "/"))
			if n == "" || seen[n] || !pass(n) {
				continue
			}
			seen[n] = true
			out = append(out, cm)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// staticCompletion fills one adapter-listed command, with its choices when
// it is a choice.
func (e *Engine) staticCompletion(ctx context.Context, s *sess, sc spec.StaticCommand) harness.Completion {
	comp := harness.Completion{Insert: "/" + sc.Name + " ", Title: "/" + sc.Name, Detail: sc.Detail, Hint: sc.Hint,
		Kind: s.ad.CommandKind(sc), Role: sc.Role, Action: sc.Action, Opens: sc.Opens}
	if sc.Screen != "" {
		if _, ok := s.ad.Screens[sc.Screen]; ok {
			comp.Screen = sc.Screen
		}
	}
	if act, ok := s.ad.Actions[sc.Action]; ok && comp.Role == "" && act.Role == harness.RoleDestructive {
		// A command that runs a destructive action asks first, like the action.
		comp.Role = spec.RoleDestructive
	}
	if comp.Kind != spec.CommandChoice {
		return comp
	}
	var choices []harness.CommandChoice
	var action string
	if s.ad.ModeAction(sc.Action) {
		choices, action = s.modeChoices(), sc.Action
	} else if comp.Screen != "" {
		choices, action = e.screenChoices(ctx, s, comp.Screen)
	}
	if len(choices) == 0 {
		// Nothing to pick from in place: push the screen, or type it.
		comp.Kind = spec.CommandPicker
		if comp.Screen == "" {
			comp.Kind = spec.HintKind(sc.Hint)
		}
		return comp
	}
	comp.Choices, comp.Action = choices, action
	return comp
}

// modeChoices are the selectable modes with the current one selected.
func (s *sess) modeChoices() []harness.CommandChoice {
	s.mu.Lock()
	cur := s.currentModeLocked()
	s.mu.Unlock()
	var out []harness.CommandChoice
	for _, m := range s.ad.Modes.Choices {
		out = append(out, harness.CommandChoice{ID: m.ID, Label: m.Label, Selected: m.ID == cur})
	}
	return out
}

// screenChoices reads a short screen's values: a one-field choice settings
// screen, or a picker over a static list. action is what a pick runs.
func (e *Engine) screenChoices(ctx context.Context, s *sess, name string) (choices []harness.CommandChoice, action string) {
	sc := s.ad.Screens[name]
	if sc.Capability != "" && !harness.Has(s.ad.Capabilities, sc.Capability) {
		return nil, ""
	}
	scr, err := e.buildScreen(ctx, s, name, sc, nil)
	if err != nil {
		return nil, ""
	}
	switch {
	case scr.Picker != nil:
		for _, it := range scr.Picker.Items {
			if it.Disabled {
				continue
			}
			label := it.Title
			if it.Badge != "" && !strings.Contains(label, it.Badge) {
				label += " " + it.Badge
			}
			choices = append(choices, harness.CommandChoice{ID: it.ID, Label: label, Selected: it.ID == scr.Picker.Selected})
		}
		action = scr.Picker.OnSelect
	case scr.Settings != nil && len(scr.Settings.Sections) == 1 && len(scr.Settings.Sections[0].Fields) == 1:
		f := scr.Settings.Sections[0].Fields[0]
		cur := toString(decodeRaw(f.Value))
		for _, o := range f.Options {
			choices = append(choices, harness.CommandChoice{ID: o.ID, Label: o.Label, Selected: o.ID == cur})
		}
		action = f.Action
	}
	if len(choices) > harness.MaxChoices {
		return nil, ""
	}
	return choices, action
}

// sourceCompletions lists the harness's own commands. They type their text
// unless they say otherwise: run, or args when they take an argument.
func (e *Engine) sourceCompletions(ctx context.Context, s *sess, c *spec.Commands) []harness.Completion {
	data, _, err := e.fetch(ctx, s, c.Source, nil, false)
	if err != nil {
		return nil
	}
	list := data
	if c.Select != "" {
		s.mu.Lock()
		st := s.stateLocked()
		s.mu.Unlock()
		if v, err := JQFirst(c.Select, data, st); err == nil {
			list = v
		}
	}
	arr, _ := list.([]any)
	var out []harness.Completion
	for _, el := range arr {
		m, _ := el.(map[string]any)
		name := toString(m["name"])
		if name == "" {
			continue
		}
		hint := toString(m["hint"])
		kind := toString(m["kind"])
		if !slices.Contains([]string{spec.CommandRun, spec.CommandArgs, spec.CommandTerminal}, kind) {
			kind = spec.HintKind(hint)
		}
		out = append(out, harness.Completion{Insert: "/" + name + " ", Title: "/" + name,
			Detail: toString(m["detail"]), Hint: hint, Kind: kind})
	}
	return out
}

func decodeRaw(raw []byte) any {
	v, _ := decode(raw)
	return v
}
