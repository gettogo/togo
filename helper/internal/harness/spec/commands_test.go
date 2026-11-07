package spec

import (
	"strings"
	"testing"
)

// commandsDoc is a small adapter with every command key in use.
const commandsDoc = `
adapter: cmds
name: Commands
revision: 1
match: { agents: [cmds] }
harness: { version: [cmds, --version], pattern: '(\S+)', range: ">=1", stderr: merge }
sources:
  models:
    static: [{ id: a }, { id: b }]
    stability: documented
  sessions:
    cli: { argv: [cmds, sessions], stderr: merge }
    stability: documented
screens:
  model: { primitive: picker, title: Model, source: models, on_select: set_model }
  resume: { primitive: picker, title: Resume, source: sessions, on_select: resume }
  effort:
    primitive: settings
    title: Effort
    fields: [{ id: effort, label: Effort, kind: choice, options: [low, high], action: set_effort }]
  prefs:
    primitive: settings
    title: Settings
    fields:
      - { id: a, label: A, kind: toggle }
      - { id: b, label: B, kind: choice, options: [x, y] }
actions:
  set_model: { relaunch: { argv: [cmds, -m, '{{.id}}'] }, stability: documented }
  set_effort: { type: { text: '/effort {{.value}}' }, stability: documented-command }
  resume:
    branch:
      - { when: '{{.state.idle}}', type: { text: '/resume {{.id}}' } }
      - { relaunch: { argv: [cmds, --resume, '{{.id}}'] } }
    stability: documented
  compact: { confirm: 'Compact now?', type: { text: '/compact' }, effect: relaunch, stability: documented-command }
status:
  actions: [compact]
commands:
  static:
    - { name: model, screen: model }
    - { name: effort, screen: effort }
    - { name: resume, screen: resume }
    - { name: settings, screen: prefs }
    - { name: add, hint: '<file>' }
    - { name: review, hint: '[pr]' }
    - { name: clear, role: destructive }
    - { name: compact, action: compact }
    - { name: stats, opens: status }
    - { name: diff, opens: changes }
    - { name: tools, kind: terminal }
    - { name: pick, kind: picker, screen: model }
`

func TestParseCommandKeys(t *testing.T) {
	a, err := Parse([]byte(commandsDoc))
	if err != nil {
		t.Fatal(err)
	}
	if a.Harness.Stderr != StderrMerge || a.Sources["sessions"].CLI.Stderr != StderrMerge {
		t.Errorf("stderr not read: %+v", a.Harness)
	}
	if a.Status == nil || len(a.Status.Actions) != 1 || a.Status.Actions[0] != "compact" {
		t.Errorf("status %+v", a.Status)
	}
	want := map[string]string{
		"model": CommandChoice, "effort": CommandChoice, "resume": CommandPicker, "settings": CommandPicker,
		"add": CommandArgs, "review": CommandRun, "clear": CommandRun, "compact": CommandRun,
		"stats": CommandRun, "diff": CommandRun, "tools": CommandTerminal, "pick": CommandPicker,
	}
	for _, c := range a.Commands.Static {
		if got := a.CommandKind(c); got != want[c.Name] {
			t.Errorf("/%s kind %q, want %q", c.Name, got, want[c.Name])
		}
	}
	for name, eff := range map[string]string{"set_model": EffectRelaunch, "resume": EffectRelaunch, "set_effort": "", "compact": EffectRelaunch} {
		act := a.Actions[name]
		if got := act.ActionEffect(); got != eff {
			t.Errorf("%s effect %q, want %q", name, got, eff)
		}
	}
}

func TestParseCommandRejects(t *testing.T) {
	for name, c := range map[string]struct{ from, to, want string }{
		"bad kind":          {"{ name: tools, kind: terminal }", "{ name: tools, kind: menu }", `kind "menu" is not one of`},
		"bad role":          {"{ name: clear, role: destructive }", "{ name: clear, role: primary }", `role "primary" is not destructive`},
		"unknown action":    {"{ name: compact, action: compact }", "{ name: compact, action: squash }", `unknown action "squash"`},
		"unknown screen":    {"{ name: model, screen: model }", "{ name: model, screen: models }", `unknown screen "models"`},
		"bad opens":         {"{ name: stats, opens: status }", "{ name: stats, opens: usage }", `opens "usage"`},
		"exclusive":         {"{ name: stats, opens: status }", "{ name: stats, opens: status, action: compact }", "exclusive"},
		"choice no screen":  {"{ name: tools, kind: terminal }", "{ name: tools, kind: choice }", "needs a screen or the modes action"},
		"choice not mode":   {"{ name: tools, kind: terminal }", "{ name: tools, kind: choice, action: compact }", "needs a screen or the modes action"},
		"picker no screen":  {"{ name: tools, kind: terminal }", "{ name: tools, kind: picker }", "kind picker needs a screen"},
		"terminal + screen": {"{ name: pick, kind: picker, screen: model }", "{ name: pick, kind: terminal, screen: model }", "types the command"},
		"twice":             {"{ name: diff, opens: changes }", "{ name: stats, opens: changes }", "listed twice"},
		"status action":     {"actions: [compact]", "actions: [squash]", `status.actions: unknown action "squash"`},
		"effect":            {"effect: relaunch", "effect: reboot", `effect "reboot"`},
		"gate stderr":       {"stderr: merge }", "stderr: keep }", `harness.stderr: "keep"`},
		"cli stderr":        {"stderr: merge }\n    stability", "stderr: drop }\n    stability", `stderr "drop" is not merge`},
		"unknown key":       {"{ name: tools, kind: terminal }", "{ name: tools, knd: terminal }", "field knd not found"},
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(commandsDoc, c.from) {
				t.Fatalf("doc lacks %q", c.from)
			}
			_, err := Parse([]byte(strings.Replace(commandsDoc, c.from, c.to, 1)))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
		})
	}
}

func TestHintKind(t *testing.T) {
	for hint, want := range map[string]string{"": CommandRun, "[focus]": CommandRun, "<file>": CommandArgs, " <name>": CommandArgs} {
		if got := HintKind(hint); got != want {
			t.Errorf("HintKind(%q) = %q, want %q", hint, got, want)
		}
	}
}
