package auggie

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileLoads(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Feeds["hooks"].Stability != spec.StableUnverified {
		t.Fatal("hook feed must say it is unverified")
	}
	var logs []string
	engine.New(engine.Config{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	for _, l := range logs {
		if strings.Contains(l, "auggie") {
			t.Fatal(l)
		}
	}
}

// The fixture follows the payloads the 0.37 bundle builds for each hook.
func TestHookPayloads(t *testing.T) {
	f, err := os.Open("testdata/hooks.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m := NewHooks(harness.Env{Cwd: "/Users/user/proj"})
	items := map[string]harness.Item{}
	sc := bufio.NewScanner(f)
	t0 := time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC)
	for i := 0; sc.Scan(); i++ {
		out, _ := m.Map(harness.Record{At: t0.Add(time.Duration(i) * time.Second), Data: json.RawMessage(sc.Text())})
		for _, it := range out {
			items[it.ID] = it
		}
	}
	k := map[string][]harness.Item{}
	for _, it := range items {
		k[it.Kind] = append(k[it.Kind], it)
	}
	if len(k[harness.KindUser]) != 1 || k[harness.KindUser][0].User.Text != "Rename the config loader" {
		t.Fatalf("user %+v", k[harness.KindUser])
	}
	if len(k[harness.KindTool]) != 3 || len(k[harness.KindAssistant]) != 1 || len(k[harness.KindNotice]) != 1 {
		t.Fatalf("kinds %v", len(items))
	}
	byName := map[string]*harness.ToolBody{}
	for _, it := range k[harness.KindTool] {
		byName[it.Tool.Name] = it.Tool
	}
	if v := byName["view"]; v.State != harness.StateOK || v.Category != harness.ToolRead || v.Title != "view config.go" {
		t.Fatalf("view %+v", v)
	}
	if e := byName["str-replace-editor"]; e.Category != harness.ToolEdit || len(e.Files) != 1 || e.Files[0].Path != "config.go" {
		t.Fatalf("edit %+v", e)
	}
	if p := byName["launch-process"]; p.State != harness.StateFailed || p.Category != harness.ToolShell || p.Output != "exit status 1" {
		t.Fatalf("process %+v", p)
	}
	tu := k[harness.KindTurn][0].Turn
	if tu.State != harness.StateOK || tu.Steps != 3 || tu.FilesChanged != 1 {
		t.Fatalf("turn %+v", tu)
	}
	if st := m.State(); st["harness_session"] != "c0ffee00-1111-4222-8333-444455556666" || st["working"] != "false" {
		t.Fatalf("state %v", st)
	}
}

func TestMalformed(t *testing.T) {
	m := NewHooks(harness.Env{})
	for _, l := range []string{``, `{`, `{"hook_event_name":"PostToolUse","tool_name":"x"}`, `{"hook_event_name":"Stop"}`} {
		m.Map(harness.Record{Data: json.RawMessage(l)})
	}
}

func TestInitialModeKnown(t *testing.T) {
	if got := NewHooks(harness.Env{Argv: []string{"auggie"}}).State()["mode"]; got != "code" {
		t.Errorf("mode = %q, want code", got)
	}
	if got := NewHooks(harness.Env{Argv: []string{"auggie", "--ask"}}).State()["mode"]; got != "ask" {
		t.Errorf("mode with --ask = %q, want ask", got)
	}
}
