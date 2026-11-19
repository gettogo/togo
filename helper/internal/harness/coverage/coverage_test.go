package coverage

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/harness"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/amp"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/claude"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/pi"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/shell"
)

// TestCoverageRatchet is the gate: every cell without tape or generated
// evidence is listed in missing.txt, and nothing else is.
func TestCoverageRatchet(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := ReadMissing(MissingFile())
	if err != nil {
		t.Fatal(err)
	}
	if problems := m.CheckRatchet(listed); len(problems) > 0 {
		t.Fatalf("the coverage list is out of date:\n  %s\nSee it with: make coverage-matrix. Shrink the list with: go run ./internal/harness/coverage/cmd/coverage-report -update",
			strings.Join(problems, "\n  "))
	}
}

// The cells come from the files on disk and the registry, so they must
// agree with what the helper registers at run time.
func TestCellsMatchRegisteredAdapters(t *testing.T) {
	cells, err := Cells()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, c := range cells {
		have[c.Agent] = true
	}
	terminal, err := TerminalAgents()
	if err != nil {
		t.Fatal(err)
	}
	if !have[TerminalAgent] {
		t.Error("the terminal row has no cells")
	}
	for _, a := range agents.Registry {
		if have[a.ID] == slices.Contains(terminal, a.ID) {
			t.Errorf("registry agent %s is exactly one of: a row of its own, or the terminal row (row %v, terminal %v)", a.ID, have[a.ID], slices.Contains(terminal, a.ID))
		}
	}
	for id, b := range harness.AdapterFiles() {
		if id == "opencode2" {
			continue // a second file for the opencode agent
		}
		if len(b) > 0 && !have[id] && !slices.Contains(terminal, id) {
			t.Errorf("registered adapter %s has no cells and is not in the terminal row", id)
		}
	}
}

// The supported agents are the ones whose adapters declare chat; the rest
// share the one terminal row.
func TestRowsAreTheAgentsWithChat(t *testing.T) {
	cells, err := Cells()
	if err != nil {
		t.Fatal(err)
	}
	rows := Agents(cells)
	want := []string{"aider", "amp", "claude", "cline", "codex", "copilot", "crush", "cursor", "droid", "goose",
		"kilo", "kimi", "omp", "opencode", "pi", TerminalAgent, "vibe"}
	if !slices.Equal(rows, want) {
		t.Errorf("rows %v, want %v", rows, want)
	}
	terminal, err := TerminalAgents()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"auggie", "kiro", "openhands", "shell"} {
		if !slices.Contains(terminal, id) {
			t.Errorf("%s is not in the terminal row", id)
		}
	}
	idx := Index(cells)
	for _, ev := range terminalLifecycle {
		if c := idx[TerminalAgent+"/lifecycle/"+ev]; c.NotApplicable != "" {
			t.Errorf("terminal %s does not apply: %s", ev, c.NotApplicable)
		}
	}
	for _, k := range []string{"terminal/capability/screen_prompt"} {
		if c, ok := idx[k]; !ok || c.NotApplicable != "" {
			t.Errorf("%s: want an applicable cell, got %+v", k, c)
		}
	}
	for _, k := range []string{"terminal/lifecycle/resume", "terminal/lifecycle/turn_end", "terminal/scale/at_limit"} {
		if c := idx[k]; c.NotApplicable == "" {
			t.Errorf("%s must not apply to the terminal row", k)
		}
	}
}

const tmpl = `adapter: %[1]s
name: %[1]s
revision: 1
match: { agents: [%[1]s], commands: [%[1]s] }
harness: { version: [%[1]s, --version], pattern: '(\d+\.\d+\.\d+)', range: ">=1.0 <3" }
capabilities: [%[2]s]
`

func writeSpec(t *testing.T, dir, id, caps string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id, id+".yaml"), []byte(fmt.Sprintf(tmpl, id, caps)), 0o644); err != nil {
		t.Fatal(err)
	}
}

var testAgents = []agents.Agent{
	{ID: "alpha", Name: "Alpha", Command: "alpha", Resume: []string{"--continue"}},
	{ID: "beta", Name: "Beta", Command: "beta"},
}

func keysOf(cells []Cell, applicableOnly bool) []string {
	var out []string
	for _, c := range cells {
		if !applicableOnly || c.NotApplicable == "" {
			out = append(out, c.Key())
		}
	}
	return out
}

func TestCellsFromTempSpecs(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "alpha", "chat, interrupt, resume, model")
	writeSpec(t, dir, "beta", "")
	cells, err := CellsFrom(dir, testAgents)
	if err != nil {
		t.Fatal(err)
	}
	idx := Index(cells)
	for _, k := range []string{"alpha/capability/chat", "alpha/capability/model", "alpha/lifecycle/resume", "alpha/lifecycle/interrupt", "alpha/scale/long_session", "terminal/lifecycle/helper_restart", "terminal/capability/screen_prompt"} {
		if c, ok := idx[k]; !ok || c.NotApplicable != "" {
			t.Errorf("%s: want an applicable cell, got %+v (present %v)", k, c, ok)
		}
	}
	for _, k := range []string{"terminal/lifecycle/resume", "terminal/lifecycle/turn_end", "terminal/scale/at_limit", "alpha/lifecycle/fork"} {
		c, ok := idx[k]
		if !ok || c.NotApplicable == "" {
			t.Errorf("%s: want a not-applicable cell with a reason, got %+v (present %v)", k, c, ok)
		}
	}
	for k := range idx {
		if strings.HasPrefix(k, "beta/") {
			t.Errorf("%s: beta declares no chat, so it has no row (the terminal row stands for it)", k)
		}
	}
	// keys come out sorted and unique
	if !slices.IsSorted(keysOf(cells, false)) {
		t.Error("cells are not sorted by key")
	}
}

func TestRatchetOnTempSpecs(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "alpha", "chat, interrupt")
	cells, err := CellsFrom(dir, testAgents[:1])
	if err != nil {
		t.Fatal(err)
	}
	ev := []Evidence{
		{Key: "alpha/capability/chat", Tier: TierTape, File: "x_test.go", Line: 1},
		{Key: "alpha/capability/interrupt", Tier: TierUnit, File: "x_test.go", Line: 2},
	}
	m := NewMatrix(cells, ev)
	listed := m.Missing()
	if slices.Contains(listed, "alpha/capability/chat") || !slices.Contains(listed, "alpha/capability/interrupt") {
		t.Fatalf("tape covers a cell, unit alone does not: %v", listed)
	}
	if p := m.CheckRatchet(listed); len(p) != 0 {
		t.Fatalf("a list built from the matrix must pass: %v", p)
	}

	// a capability lands: a new cell appears and the old list fails
	writeSpec(t, dir, "alpha", "chat, interrupt, files")
	cells2, _ := CellsFrom(dir, testAgents[:1])
	p := NewMatrix(cells2, ev).CheckRatchet(listed)
	if len(p) != 1 || !strings.Contains(p[0], "alpha/capability/files") || !strings.Contains(p[0], "not in missing.txt") {
		t.Fatalf("a new capability must be reported as an addition to make: %v", p)
	}

	// a capability is removed: the stale line is reported
	writeSpec(t, dir, "alpha", "chat")
	cells3, _ := CellsFrom(dir, testAgents[:1])
	p = NewMatrix(cells3, ev).CheckRatchet(listed)
	if len(p) == 0 || !strings.Contains(strings.Join(p, "\n"), "alpha/capability/interrupt, which is not a cell") {
		// interrupt is gone as a capability, but the lifecycle cell is not applicable too
		if !strings.Contains(strings.Join(p, "\n"), "alpha/capability/interrupt") {
			t.Fatalf("a removed capability must be reported as a stale line: %v", p)
		}
	}

	// a listed cell gains tape evidence: the line must go
	more := append(slices.Clone(ev), Evidence{Key: "alpha/capability/interrupt", Tier: TierGenerated, File: "y_test.go", Line: 3})
	p = NewMatrix(cells, more).CheckRatchet(listed)
	if len(p) != 1 || !strings.Contains(p[0], "alpha/capability/interrupt") || !strings.Contains(p[0], "remove the line") {
		t.Fatalf("a covered cell must be reported for removal: %v", p)
	}
}

func TestRatchetRejectsBadLists(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "alpha", "chat")
	cells, _ := CellsFrom(dir, testAgents[:1])
	m := NewMatrix(cells, nil)
	good := m.Missing()
	bad := append(slices.Clone(good), "alpha/capability/nope", "alpha/lifecycle/resume")
	slices.Sort(bad)
	p := strings.Join(m.CheckRatchet(bad), "\n")
	if !strings.Contains(p, "alpha/capability/nope, which is not a cell") || !strings.Contains(p, "alpha/lifecycle/resume, which is not applicable") {
		t.Fatalf("unknown and not-applicable lines must be reported: %s", p)
	}
	unsorted := []string{good[1], good[0]}
	if p := m.CheckRatchet(append(unsorted, good[2:]...)); !strings.Contains(strings.Join(p, "\n"), "not sorted") {
		t.Fatalf("an unsorted list must be reported: %v", p)
	}
	m2 := NewMatrix(cells, []Evidence{{Key: "alpha/capability/typo", Tier: TierTape, File: "z_test.go", Line: 9}})
	if p := m2.UnknownEvidence(); len(p) != 1 || !strings.Contains(p[0], "z_test.go:9") {
		t.Fatalf("evidence for an unknown cell must be reported: %v", p)
	}
}

func TestSpecForUnknownAgentIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "gamma", "chat")
	if _, err := CellsFrom(dir, testAgents); err == nil || !strings.Contains(err.Error(), "gamma") {
		t.Fatalf("want an error naming gamma, got %v", err)
	}
}

func TestWriteReadMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.txt")
	if err := WriteMissing(p, []string{"b/x/y", "a/x/y"}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMissing(p)
	if err != nil || !slices.Equal(got, []string{"a/x/y", "b/x/y"}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestScanEvidence(t *testing.T) {
	ev, err := ScanEvidence("testdata/scan")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ev {
		got = append(got, e.Tier+" "+e.Key)
	}
	want := []string{"tape pi/capability/chat", "tape pi/lifecycle/turn_end", "unit omp/scale/at_limit"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if ev[0].File != "sample.go" || ev[0].Line != 10 {
		t.Fatalf("position: %+v", ev[0])
	}
	if _, err := ScanEvidence("testdata/bad"); err == nil || !strings.Contains(err.Error(), "literal") {
		t.Fatalf("a non-literal key must be an error, got %v", err)
	}
}

func TestScanSkipsTestdataBelowRoot(t *testing.T) {
	root := t.TempDir()
	bad, _ := os.ReadFile("testdata/bad/bad.go")
	if err := os.MkdirAll(filepath.Join(root, "pkg", "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "testdata", "bad.go"), bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if ev, err := ScanEvidence(root); err != nil || len(ev) != 0 {
		t.Fatalf("testdata below the root is skipped: %v %v", ev, err)
	}
}

type fakeTB struct {
	testing.TB
	failed string
}

func (f *fakeTB) Helper() {}
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = format
	panic(f)
}

func catch(f func()) (failed bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(*fakeTB); !ok {
				panic(r)
			}
			failed = true
		}
	}()
	f()
	return false
}

func TestCoversValidatesKeys(t *testing.T) {
	if catch(func() { Covers(&fakeTB{}, "unit", "pi/capability/chat") }) {
		t.Error("a real cell must pass")
	}
	for _, c := range []struct{ tier, key string }{
		{"unit", "pi/capability/chta"},        // typo
		{"unit", "terminal/lifecycle/resume"}, // not applicable
		{"fast", "pi/capability/chat"},        // bad tier
	} {
		if !catch(func() { Covers(&fakeTB{}, c.tier, c.key) }) {
			t.Errorf("%+v must fail the test", c)
		}
	}
	if !catch(func() { Covers(&fakeTB{}, "unit") }) {
		t.Error("no keys must fail")
	}
}

func TestConformanceEvidence(t *testing.T) {
	cells, err := Cells()
	if err != nil {
		t.Fatal(err)
	}
	nightly, err := NightlyAgents(filepath.Join(HelperDir(), ".."))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(nightly, "opencode") || slices.Contains(nightly, "claude") {
		t.Fatalf("nightly agents: %v", nightly)
	}
	ev := ConformanceEvidence(cells, nightly)
	if len(ev) == 0 {
		t.Fatal("no conformance evidence")
	}
	for _, e := range ev {
		if e.Tier != TierLive || strings.HasPrefix(e.Key, "claude/") {
			t.Errorf("unexpected %+v", e)
		}
	}
}

// Every scenario the table names must exist in the conformance command.
func TestConformanceTableNamesRealScenarios(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(HelperDir(), "cmd", "togo-conformance", "scenarios.go"))
	if err != nil {
		t.Fatal(err)
	}
	list := string(b)
	list = list[strings.Index(list, "var scenarios = []string{"):]
	list = list[:strings.Index(list, "}")]
	for capability, sc := range conformanceCapabilities {
		if !strings.Contains(list, `"`+sc+`"`) {
			t.Errorf("capability %s maps to scenario %q, which togo-conformance does not have", capability, sc)
		}
	}
}
