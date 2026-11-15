package engine

import (
	"slices"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// A harness's command API may leave commands out: omp's lists only the
// commands that can run without its interactive screen, so /resume, /new,
// /settings and some thirty more are missing from it. Where the running
// program publishes its own list, that list is the catalog. The command API
// still adds what the program's list does not carry (argument hints,
// aliases, subcommands, where a command comes from), and the adapter's
// intents say what each command does when run.

// mergeLive makes the program's own list the catalog. The adapter's static
// commands keep their native routes and come first; then the program's
// commands in its own order; then any the command API has that the program
// did not list, unchanged and without Native.
func mergeLive(c *spec.Commands, all []harness.Completion, rows []map[string]any, live []harness.LiveCommand) []harness.Completion {
	name := func(cm harness.Completion) string { return strings.TrimPrefix(cm.Title, "/") }
	meta := map[string]map[string]any{}
	for _, m := range rows {
		meta[toString(m["name"])] = m
	}
	listed := map[string]harness.LiveCommand{}
	for _, l := range live {
		if !l.Folder {
			listed[l.Name] = l
		}
	}
	placed := map[string]bool{}
	// The program lists an alias as a command of its own (/quit beside
	// /exit). It is the same command: it is listed once, under its name,
	// and the alias finds it.
	for _, m := range rows {
		list, _ := m["aliases"].([]any)
		for _, a := range list {
			if alias := toString(a); alias != "" && alias != toString(m["name"]) {
				if _, isCommand := meta[alias]; !isCommand {
					placed[alias] = true
				}
			}
		}
	}
	var out []harness.Completion
	static := len(c.Static)
	if static > len(all) {
		static = len(all)
	}
	for _, cm := range all[:static] {
		if l, ok := listed[name(cm)]; ok {
			cm.Native = native(c, l, meta[l.Name])
		}
		placed[name(cm)] = true
		out = append(out, cm)
	}
	byName := map[string]harness.Completion{}
	for _, cm := range all[static:] {
		byName[name(cm)] = cm
	}
	for _, l := range live {
		if l.Folder || placed[l.Name] {
			continue
		}
		placed[l.Name] = true
		cm, ok := byName[l.Name]
		if !ok {
			cm = harness.Completion{Insert: "/" + l.Name + " ", Title: "/" + l.Name, Kind: spec.CommandRun}
		}
		if l.Detail != "" {
			cm.Detail = l.Detail
		}
		cm.Native = native(c, l, meta[l.Name])
		if cm.Native.Destructive && cm.Role == "" {
			cm.Role = harness.RoleDestructive
		}
		out = append(out, cm)
	}
	for _, cm := range all[static:] {
		if !placed[name(cm)] {
			placed[name(cm)] = true
			out = append(out, cm)
		}
	}
	return out
}

// native describes one of the program's commands: its own state, icon and
// group, what the command API adds, and the adapter's intent for it.
func native(c *spec.Commands, l harness.LiveCommand, m map[string]any) *harness.CommandNative {
	n := &harness.CommandNative{State: l.State, Icon: l.Icon, Group: l.Group, Source: toString(m["source"])}
	if list, _ := m["aliases"].([]any); len(list) > 0 {
		for _, a := range list {
			if s := toString(a); s != "" && len(n.Aliases) < harness.MaxSubcommands {
				n.Aliases = append(n.Aliases, s)
			}
		}
	}
	if list, _ := m["subcommands"].([]any); len(list) > 0 {
		for _, el := range list {
			sub, _ := el.(map[string]any)
			if s := toString(sub["name"]); s != "" && len(n.Subcommands) < harness.MaxSubcommands {
				n.Subcommands = append(n.Subcommands, harness.CommandSub{Name: s, Detail: toString(sub["detail"]), Hint: toString(sub["hint"])})
			}
		}
	}
	if n.Source == "" && l.Group != "" {
		n.Source = strings.TrimSuffix(l.Group, ":")
	}
	intent, ok := c.Intents[l.Name]
	if !ok && n.Source != "" {
		intent, ok = c.Intents["source:"+n.Source]
	}
	n.Opens, n.OpensWithArgs, n.Args, n.Destructive, n.ScreenID = intent.Opens, intent.OpensWithArgs, intent.Args, intent.Destructive, intent.Screen
	if n.Opens == "" {
		n.Opens = harness.OpensUnknown
	}
	// A command that shows something names what it shows, so the app can
	// tell one picker from another: the command's own name unless the
	// adapter gives one (/resume and /switch are both pickers).
	if n.ScreenID == "" && slices.Contains([]string{harness.OpensPicker, harness.OpensSettings, harness.OpensSignIn,
		harness.OpensScreen, harness.OpensInput, harness.OpensText}, n.Opens) {
		n.ScreenID = l.Name
	}
	if n.Args == "" {
		switch hint := strings.TrimSpace(toString(m["hint"])); {
		case strings.HasPrefix(hint, "<"):
			n.Args = "required"
		case hint != "" || len(n.Subcommands) > 0:
			n.Args = "optional"
		}
	}
	return n
}

// aliasPasses reports whether one of the command's aliases matches the
// query, so "/quit" finds the row listed as "/exit".
func aliasPasses(cm harness.Completion, q string, pass func(string) bool) bool {
	if cm.Native == nil || q == "" {
		return false
	}
	for _, a := range cm.Native.Aliases {
		if pass(strings.ToLower(a)) {
			return true
		}
	}
	return false
}
