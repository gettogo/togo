package omp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Jobs are keyed by the name `omp ps stop` takes; a global service's id
// carries its scope, and Stop passes it as --global.
func TestJobsStopByName(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	src := a.Sources["jobs"]
	if src.Timeout.Seconds() != 15 {
		t.Errorf("timeout %v", src.Timeout)
	}
	var in any
	json.Unmarshal([]byte(`[{"kind":"project","daemons":[{"name":"dev","command":"npm run dev","state":"running"}]},
		{"kind":"global","scope":"relay","daemons":[{"name":"browser","state":"running"}]}]`), &in)
	out, err := engine.JQ(src.Select, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := out[0].([]any)
	if rows[0].(map[string]any)["rowId"] != "dev" || rows[1].(map[string]any)["rowId"] != "global:relay:browser" {
		t.Fatalf("rows %v", rows)
	}
	if sub, _ := engine.MapValue(a.Mission["background"].Map["item.subtitle"], rows[1], nil); sub != "running" {
		t.Errorf("subtitle %q", sub)
	}
	stop := a.Actions["stop"].Branch
	cond, _ := engine.Render(stop[0].When, map[string]any{"target": "global:relay:browser"})
	if cond != "true" {
		t.Fatal("global branch not taken")
	}
	if c, _ := engine.Render(stop[0].When, map[string]any{"target": "dev"}); c != "" {
		t.Fatal("project job took the global branch")
	}
	var argv []string
	for _, x := range stop[0].CLI.Argv {
		r, _ := engine.Render(x, map[string]any{"target": "global:relay:browser"})
		argv = append(argv, r)
	}
	if want := "omp ps stop browser --global=relay"; strings.Join(argv, " ") != want {
		t.Errorf("argv %q, want %q", strings.Join(argv, " "), want)
	}
}
