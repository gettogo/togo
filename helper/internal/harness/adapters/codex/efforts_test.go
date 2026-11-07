package codex

import (
	"encoding/json"
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
