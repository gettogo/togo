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

func TestParseRejectsHugeFile(t *testing.T) {
	if _, err := Parse(make([]byte, MaxFile+1)); err == nil {
		t.Fatal("accepted an oversized file")
	}
}
