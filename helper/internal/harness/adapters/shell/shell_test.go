package shell

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/tap"
)

func rec(t *testing.T, m tap.Mark, at int64) harness.Record {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return harness.Record{Source: "marks", At: time.Unix(at, 0), Data: b}
}

func code(n int) *int { return &n }

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "shell" || a.Feeds["marks"].Mapper != MapperMarks || a.Feeds["marks"].Hook.Events[0] != Event {
		t.Fatalf("adapter %+v", a)
	}
	// The command block view is opt-in.
	if harness.Has(a.Capabilities, harness.CapChat) {
		t.Fatalf("chat should be off by default: %v", a.Capabilities)
	}
	if f, ok := harness.LookupMapper(MapperMarks); !ok || f(harness.Env{}).Name() != MapperMarks {
		t.Fatal("mapper not registered")
	}
	if _, ok := harness.AdapterFiles()["shell"]; !ok {
		t.Fatal("adapter file not registered")
	}
}

func TestBlockRunsThenFinishesInPlace(t *testing.T) {
	m := newMarks(harness.Env{Cwd: "/home/me"})
	items, _ := m.Map(rec(t, tap.Mark{Mark: tap.MarkStart, Command: "go test ./..."}, 10))
	if len(items) != 1 {
		t.Fatalf("items %+v", items)
	}
	start := items[0]
	if start.Kind != harness.KindShell || start.Shell.State != harness.StateRunning || start.Shell.Cwd != "/home/me" {
		t.Fatalf("start %+v", start.Shell)
	}
	if m.State()["working"] != "true" {
		t.Fatal("not working while a command runs")
	}
	items, _ = m.Map(rec(t, tap.Mark{Mark: tap.MarkEnd, Exit: code(1), Output: "FAIL"}, 14))
	if len(items) != 1 || items[0].ID != start.ID {
		t.Fatalf("end %+v", items)
	}
	end := items[0].Shell
	if end.State != harness.StateFailed || *end.ExitCode != 1 || end.Output != "FAIL" || end.Command != "go test ./..." {
		t.Fatalf("end %+v", end)
	}
	if !end.Started.Equal(time.Unix(10, 0)) || !end.Ended.Equal(time.Unix(14, 0)) {
		t.Fatalf("times %v %v", end.Started, end.Ended)
	}
	if m.State()["working"] != "false" {
		t.Fatal("still working")
	}
	// The running copy the app already has is not changed by the update.
	if start.Shell.State != harness.StateRunning {
		t.Fatal("start item mutated")
	}
}

func TestMissingMarks(t *testing.T) {
	m := newMarks(harness.Env{})
	m.Map(rec(t, tap.Mark{Mark: tap.MarkStart, Command: "a"}, 1))
	// A second start closes the first with no status.
	items, _ := m.Map(rec(t, tap.Mark{Mark: tap.MarkStart, Command: "b", Cwd: "/x"}, 2))
	if len(items) != 2 || items[0].ID != "cmd1" || items[0].Shell.State != harness.StateOK || items[0].Shell.ExitCode != nil {
		t.Fatalf("items %+v", items)
	}
	if items[1].ID != "cmd2" || items[1].Shell.Cwd != "/x" {
		t.Fatalf("second %+v", items[1])
	}
	// An end with no start and no command is nothing; with a command it is a
	// finished block.
	m = newMarks(harness.Env{})
	if items, _ := m.Map(rec(t, tap.Mark{Mark: tap.MarkEnd, Exit: code(0)}, 3)); len(items) != 0 {
		t.Fatalf("items %+v", items)
	}
	items, _ = m.Map(rec(t, tap.Mark{Mark: tap.MarkEnd, Command: "pwd", Exit: code(0)}, 3))
	if len(items) != 1 || items[0].Shell.State != harness.StateOK || items[0].Shell.Command != "pwd" {
		t.Fatalf("items %+v", items)
	}
	// Garbage is skipped.
	if items, _ := m.Map(harness.Record{Data: json.RawMessage(`{"mark":`)}); len(items) != 0 {
		t.Fatal("garbage mapped")
	}
}

func TestLimits(t *testing.T) {
	m := newMarks(harness.Env{})
	m.Map(rec(t, tap.Mark{Mark: tap.MarkStart, Command: "cat"}, 1))
	items, _ := m.Map(rec(t, tap.Mark{Mark: tap.MarkEnd, Output: strings.Repeat("é", harness.MaxToolOutput)}, 2))
	out := items[0].Shell.Output
	if len(out) > harness.MaxToolOutput || !strings.HasPrefix(out, "é") {
		t.Fatalf("output %d bytes", len(out))
	}
}
