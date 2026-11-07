package crush

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileLoads(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Endpoint != nil || len(a.Feeds) != 0 {
		t.Fatal("the fallback adapter must not start Crush with a server host")
	}
	var logs []string
	engine.New(engine.Config{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	for _, l := range logs {
		if strings.Contains(l, "crush") {
			t.Fatal(l)
		}
	}
}

// `crush session list --json` output (0.100.0) becomes resume rows, newest first.
func TestSessionRows(t *testing.T) {
	a, _ := spec.Parse(AdapterFile())
	b, err := os.ReadFile("testdata/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	var in any
	json.Unmarshal(b, &in)
	out, err := engine.JQ(a.Sources["sessions"].Select, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := out[0].([]any)
	first := rows[0].(map[string]any)
	if first["uuid"] != "0e3d1f22-7c41-4b0e-9d55-61a2f4c0b7aa" {
		t.Fatalf("order %v", rows)
	}
	m := a.Screens["resume"].Map
	if id, err := engine.MapValue(m["item.id"], first, nil); err != nil || id != "0e3d1f22-7c41-4b0e-9d55-61a2f4c0b7aa" {
		t.Fatalf("id %q %v", id, err)
	}
	if title, err := engine.MapValue(m["item.title"], first, nil); err != nil || title != "1a2b3c4d5e6f7a8b" {
		t.Fatalf("title %q %v", title, err)
	}
}
