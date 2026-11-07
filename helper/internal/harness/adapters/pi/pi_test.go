package pi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileLoads(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Feeds["session"].Mapper != MapperSession {
		t.Fatal("mapper")
	}
	var logs []string
	engine.New(engine.Config{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	for _, l := range logs {
		if strings.Contains(l, " pi") {
			t.Fatal(l)
		}
	}
	if f, ok := harness.LookupMapper(MapperSession); !ok || f(harness.Env{}).Name() != MapperSession {
		t.Fatal("mapper not registered")
	}
}

// The feed glob names Pi's folder for the working directory, spaces kept.
func TestSessionFolder(t *testing.T) {
	a, _ := spec.Parse(AdapterFile())
	got, err := engine.Render(a.Feeds["session"].JSONL.Glob, map[string]any{"cwd": "/Users/me/Dev/Delta RE"})
	if err != nil || got != "~/.pi/agent/sessions/--Users-me-Dev-Delta RE--/*.jsonl" {
		t.Fatalf("%q %v", got, err)
	}
}

// The model list table becomes picker rows.
func TestModelTable(t *testing.T) {
	a, _ := spec.Parse(AdapterFile())
	table := "provider       model                   context  max-out  thinking  images\n" +
		"moonshotai     kimi-k2.6               262.1K   262.1K   yes       yes   \n" +
		"anthropic      claude-sonnet-4-5       200K     64K      yes       yes   \n"
	raw, _ := json.Marshal(table)
	var in any
	json.Unmarshal(raw, &in)
	out, err := engine.JQ(a.Sources["models"].Fallback.Select, in, nil)
	if err != nil || len(out) != 1 {
		t.Fatalf("%v %v", out, err)
	}
	rows := out[0].([]any)
	first := rows[0].(map[string]any)
	if len(rows) != 2 || first["id"] != "moonshotai/kimi-k2.6" || first["thinking"] != true || first["context"] != "262.1K" {
		t.Fatalf("%v", rows)
	}
}
