package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// A fresh Codex session has no model in state yet; the Effort picker showed
// nothing (e2e group B, 0.144.1). It lists the default model's efforts.
func TestEffortsBeforeTheModelIsKnown(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	var in any
	_ = json.Unmarshal([]byte(`{"data":[
		{"id":"gpt-a","isDefault":false,"supportedReasoningEfforts":[{"reasoningEffort":"low"}]},
		{"id":"gpt-b","isDefault":true,"supportedReasoningEfforts":[{"reasoningEffort":"medium"},{"reasoningEffort":"high"}]}]}`), &in)
	sel := a.Sources["efforts"].Select
	got, err := engine.JQFirst(sel, in, map[string]string{})
	if err != nil || len(got.([]any)) != 2 {
		t.Fatalf("no model: %v %v", got, err)
	}
	got, err = engine.JQFirst(sel, in, map[string]string{"model": "gpt-a"})
	if err != nil || len(got.([]any)) != 1 {
		t.Fatalf("gpt-a: %v %v", got, err)
	}
}

// Codex has no live setter and its TUI sends its own model with every turn,
// so model and effort restart the TUI on the same thread with flags (on
// 0.167.0 a config.toml write reached only new threads). The effort a model
// switch keeps is left out while it is unknown.
func TestModelAndEffortRestartOnTheThread(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	m := a.Actions["set_model"].Branch
	if len(m) != 2 || m[0].Relaunch == nil || m[1].Relaunch == nil {
		t.Fatalf("set_model %+v", m)
	}
	data := map[string]any{"id": "gpt-x", "state": map[string]any{"harness_session": "th1", "effort": "low"}}
	var argv []string
	for _, s := range m[0].Relaunch.Argv {
		v, _ := engine.Render(s, data)
		argv = append(argv, v)
	}
	if got := strings.Join(argv, " "); got != "codex resume th1 -m gpt-x" {
		t.Fatalf("argv %q", got)
	}
	keep, _ := engine.Render(m[0].Relaunch.Keep[0][1], data)
	if keep != "model_reasoning_effort=low" {
		t.Fatalf("keep %q", keep)
	}
	if keep, _ := engine.Render(m[0].Relaunch.Keep[0][1], map[string]any{"state": map[string]any{}}); keep != "" {
		t.Fatalf("keep without effort %q", keep)
	}
	e := a.Actions["set_effort"].Branch
	if len(e) != 2 || e[0].Relaunch == nil || e[0].Relaunch.Argv[1] != "resume" {
		t.Fatalf("set_effort %+v", e)
	}
}
