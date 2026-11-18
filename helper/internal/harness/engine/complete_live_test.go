package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// The running program's own list is the catalog: it has the commands the
// command API leaves out, in the program's order, with what the API and the
// adapter's intents add. Static commands keep their native routes.
func TestTheProgramsOwnListIsTheCommandCatalog(t *testing.T) {
	c := &spec.Commands{
		Static: []spec.StaticCommand{{Name: "model", Screen: "model"}},
		Intents: map[string]spec.Intent{
			"resume":       {Opens: harness.OpensPicker, Args: "optional"},
			"exit":         {Opens: harness.OpensExit, Args: "none", Destructive: true},
			"agents":       {Opens: harness.OpensScreen, Screen: "agents"},
			"source:skill": {Opens: harness.OpensTurn},
		},
	}
	all := []harness.Completion{
		{Insert: "/model ", Title: "/model", Kind: spec.CommandPicker, Screen: "model"},
		{Insert: "/fast ", Title: "/fast", Detail: "from the API", Hint: "[on|off]", Kind: spec.CommandRun},
		{Insert: "/exit ", Title: "/exit", Kind: spec.CommandRun},
		{Insert: "/only-api ", Title: "/only-api", Kind: spec.CommandRun},
	}
	rows := []map[string]any{
		{"name": "fast", "hint": "[on|off]", "source": "builtin", "subcommands": []any{
			map[string]any{"name": "on", "detail": "Enable"}, map[string]any{"name": "", "detail": "no name"}}},
		{"name": "exit", "aliases": []any{"quit"}, "source": "builtin"},
		{"name": "model", "aliases": []any{"models"}, "source": "builtin"},
		{"name": "need", "hint": "<path>", "source": "builtin"},
	}
	live := []harness.LiveCommand{
		{Name: "skill:", Detail: "1 skill", Folder: true},
		{Name: "skill:fix", Detail: "Fix it", Group: "skill:", Icon: "skill"},
		{Name: "resume", Detail: "Resume a different session", Icon: "history"},
		{Name: "fast", Detail: "Toggle fast service", State: "off"},
		{Name: "model", Detail: "Choose", State: "provider/model"},
		{Name: "exit", Detail: "Exit the application"},
		{Name: "quit", Detail: "Exit the application"},
		{Name: "agents", Detail: "Agents"},
		{Name: "need", Detail: "Needs a path"},
		{Name: "ext", Detail: "An extension command"},
	}
	got := mergeLive(c, all, rows, live)
	var names []string
	by := map[string]harness.Completion{}
	for _, cm := range got {
		names = append(names, cm.Title)
		by[cm.Title] = cm
	}
	want := []string{"/model", "/skill:fix", "/resume", "/fast", "/exit", "/agents", "/need", "/ext", "/only-api"}
	if len(names) != len(want) {
		t.Fatalf("catalog = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("catalog = %v, want %v", names, want)
		}
	}
	if m := by["/model"]; m.Screen != "model" || m.Kind != spec.CommandPicker || m.Native == nil || m.Native.State != "provider/model" || len(m.Native.Aliases) != 1 {
		t.Errorf("static command lost its route or its native data: %+v %+v", m, m.Native)
	}
	if r := by["/resume"]; r.Native == nil || r.Native.Opens != harness.OpensPicker || r.Native.Args != "optional" || r.Native.Icon != "history" || r.Native.ScreenID != "resume" || r.Detail != "Resume a different session" || r.Kind != spec.CommandRun {
		t.Errorf("a command only the program lists: %+v %+v", r, r.Native)
	}
	f := by["/fast"]
	if f.Detail != "Toggle fast service" || f.Hint != "[on|off]" || f.Native.State != "off" || f.Native.Args != "optional" || len(f.Native.Subcommands) != 1 || f.Native.Subcommands[0].Name != "on" || f.Native.Opens != harness.OpensUnknown || f.Native.Source != "builtin" {
		t.Errorf("merged command: %+v %+v", f, f.Native)
	}
	if e := by["/exit"]; !e.Native.Destructive || e.Role != harness.RoleDestructive || e.Native.Opens != harness.OpensExit || e.Native.Args != "none" {
		t.Errorf("destructive command: %+v %+v", e, e.Native)
	}
	if a := by["/agents"]; a.Native.Opens != harness.OpensScreen || a.Native.ScreenID != "agents" {
		t.Errorf("screen command: %+v", a.Native)
	}
	if s := by["/skill:fix"]; s.Native.Group != "skill:" || s.Native.Source != "skill" || s.Native.Opens != harness.OpensTurn {
		t.Errorf("grouped command: %+v", s.Native)
	}
	if n := by["/need"]; n.Native.Args != "required" {
		t.Errorf("required argument: %+v", n.Native)
	}
	if x := by["/ext"]; x.Native.Opens != harness.OpensUnknown || x.Native.Args != "" || x.Native.ScreenID != "" || by["/exit"].Native.ScreenID != "" {
		t.Errorf("unclassified command: %+v", x.Native)
	}
	if o := by["/only-api"]; o.Native != nil {
		t.Errorf("a command the program did not list claims native data: %+v", o.Native)
	}
	// An alias finds its row.
	hit := func(n string) bool { return n == "quit" }
	if !aliasPasses(by["/exit"], "quit", hit) || aliasPasses(by["/resume"], "quit", hit) || aliasPasses(by["/exit"], "", hit) {
		t.Error("alias matching")
	}
}
