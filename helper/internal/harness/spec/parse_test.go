package spec

import (
	"os"
	"strings"
	"testing"
	"time"
)

func sample(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/models.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseSample(t *testing.T) {
	a, err := Parse(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "sample" || a.Screens["model"].Primitive != "picker" {
		t.Fatalf("parsed %+v", a)
	}
	if got := a.Sources["models"].Cache; got != 10*time.Minute {
		t.Errorf("cache %v", got)
	}
	act := a.Actions["switch_model"]
	if act.Type == nil || !act.Type.WhenIdle || act.Verify.Within != 5*time.Second {
		t.Errorf("action %+v", act)
	}
}

func TestParseRejects(t *testing.T) {
	base := string(sample(t))
	for name, c := range map[string]struct{ from, to, want string }{
		"unknown key":       {"search: true", "serach: true", "field serach not found"},
		"typed non-slash":   {"text: '/model {{.id}}'", "text: 'model {{.id}}'", "slash command"},
		"arrow keys":        {"type: { text: '/model {{.id}}', submit: true, when_idle: true }", "keys: { keys: [Down, Enter] }", `"Down" is not allowed`},
		"two kinds":         {"select: '.models[]'", "file: { path: x }\n    select: '.models[]'", "has 2 kinds"},
		"unknown action":    {"on_select: switch_model", "on_select: nope", `unknown action "nope"`},
		"unknown source":    {"source: models", "source: nope", `unknown source "nope"`},
		"no stability":      {"stability: documented-command", "stability: maybe", "stability"},
		"bad primitive":     {"primitive: picker", "primitive: wheel", "unknown primitive"},
		"cycle without max": {"type: { text: '/model {{.id}}', submit: true, when_idle: true }", "keys: { keys: [BTab], cycle: true }", "cycle needs max"},
		"until without max": {"type: { text: '/model {{.id}}', submit: true, when_idle: true }", "keys: { keys: [Escape], until: idle }", "until needs max"},
		"until other":       {"type: { text: '/model {{.id}}', submit: true, when_idle: true }", "keys: { keys: [Escape], until: done, max: 2 }", "until must be idle"},
		"until enter":       {"type: { text: '/model {{.id}}', submit: true, when_idle: true }", "keys: { keys: [Enter], until: idle, max: 2 }", "only Escape or C-c"},
		"then menu key":     {"type: { text: '/model {{.id}}', submit: true, when_idle: true }", "keys: { keys: [Escape], until: idle, max: 2, then: [Enter] }", `"Enter" is not allowed`},
		"then alone":        {"type: { text: '/model {{.id}}', submit: true, when_idle: true }", "keys: { keys: [Escape], then: [C-x, C-k] }", "then needs until"},
		"stop chord alone":  {"type: { text: '/model {{.id}}', submit: true, when_idle: true }", "keys: { keys: [C-x, C-k] }", `"C-x" is not allowed`},
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(base, c.from) {
				t.Fatalf("sample lacks %q", c.from)
			}
			_, err := Parse([]byte(strings.Replace(base, c.from, c.to, 1)))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
		})
	}
}

// An until-idle interrupt with a stop chord parses.
func TestParseUntilIdle(t *testing.T) {
	b := strings.Replace(string(sample(t)), "type: { text: '/model {{.id}}', submit: true, when_idle: true }",
		"keys: { keys: [Escape], until: idle, max: 2, then: [C-x, C-k, C-x, C-k] }", 1)
	a, err := Parse([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	k := a.Actions["switch_model"].Keys
	if k == nil || k.Until != UntilIdle || k.Max != 2 || strings.Join(k.Then, " ") != "C-x C-k C-x C-k" {
		t.Fatalf("keys %+v", k)
	}
}

func TestParseRejectsHugeFile(t *testing.T) {
	if _, err := Parse(make([]byte, MaxFile+1)); err == nil {
		t.Fatal("accepted an oversized file")
	}
}

func TestParseAcceptsUnverified(t *testing.T) {
	b := strings.Replace(string(sample(t)), "stability: documented-command", "stability: unverified", 1)
	if _, err := Parse([]byte(b)); err != nil {
		t.Fatal(err)
	}
}
