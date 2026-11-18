package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRunner(id, version string, res map[string]result) *runner {
	return &runner{id: id, version: version, res: res, took: 90 * time.Second}
}

func TestReportRoundTrips(t *testing.T) {
	o := &options{root: "/private/tmp/togo-conformance-x", router: &openRouter{model: "vendor/small"}}
	r := testRunner("opencode", "1.4.2", map[string]result{"create": {pass, ""}, "first_turn": {fail, "no reply"}})
	start := time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC)
	rep := buildReport([]*runner{r}, []string{"left alone: x"}, start, start.Add(5*time.Minute), o)
	path := filepath.Join(t.TempDir(), "r.json")
	if err := rep.write(path); err != nil {
		t.Fatal(err)
	}
	got, err := readReport(path)
	if err != nil {
		t.Fatal(err)
	}
	h := got.Harnesses[0]
	if got.Schema != 1 || got.Model != "vendor/small" || h.Account != "openrouter" || h.Seconds != 90 ||
		h.Results["first_turn"].Status != fail || !got.Finished.Equal(start.Add(5*time.Minute)) {
		t.Fatalf("round trip lost something: %+v", got)
	}
	// Older readers of the file keep working: the two keys they knew are still there.
	var raw map[string]json.RawMessage
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &raw)
	if raw["harnesses"] == nil || raw["cleanup"] == nil {
		t.Fatalf("harnesses or cleanup key missing: %s", b)
	}
}

func TestReportOmitsHomeAndRunPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home folder")
	}
	root := "/private/tmp/togo-conformance-x"
	r := testRunner("omp", "9.1", map[string]result{
		"create": {fail, "no session file under " + home + "/.omp/agent and " + root + "/homes/omp"},
	})
	r.notes = []string{"scratch " + root + "/x-w-omp", "config " + home + "/.omp/agent/config.yml left"}
	rep := buildReport([]*runner{r}, []string{"restored " + home + "/.codex/config.toml"}, time.Now(), time.Now(), &options{root: root})
	b, _ := json.Marshal(rep)
	if bytes.Contains(b, []byte(home)) || bytes.Contains(b, []byte(root)) {
		t.Fatalf("a path of this machine is in the report: %s", b)
	}
	if !strings.Contains(rep.Harnesses[0].Results["create"].Reason, "~/.omp/agent") || !strings.Contains(rep.Harnesses[0].Notes[0], "<run>/x-w-omp") {
		t.Fatalf("paths were not replaced readably: %+v", rep.Harnesses[0])
	}
}

func TestInterruptedIsNotFail(t *testing.T) {
	r := testRunner("pi", "2.0", map[string]result{"create": {pass, ""}})
	r.cutShort = true
	rep := buildReport([]*runner{r}, nil, time.Now(), time.Now(), &options{})
	res := rep.Harnesses[0].Results
	if res["create"].Status != pass || res["first_turn"].Status != interrupted || res["close"].Status != interrupted {
		t.Fatalf("results = %+v", res)
	}
	// A harness that finished leaves unreached scenarios out instead.
	done := buildReport([]*runner{testRunner("pi", "2.0", map[string]result{"create": {pass, ""}})}, nil, time.Now(), time.Now(), &options{})
	if _, ok := done.Harnesses[0].Results["first_turn"]; ok {
		t.Fatal("an unreached scenario of a finished run was given a status")
	}
	// Own account when -openrouter could not be applied, or was not asked for.
	own := testRunner("codex", "0.169", nil)
	own.ownAccount = true
	rep = buildReport([]*runner{own, r}, nil, time.Now(), time.Now(), &options{router: &openRouter{model: "m"}})
	if rep.Harnesses[0].Account != "own" || rep.Harnesses[1].Account != "openrouter" {
		t.Fatalf("accounts = %s, %s", rep.Harnesses[0].Account, rep.Harnesses[1].Account)
	}
}

func rep(version string, res map[string]string) Report {
	h := HarnessResult{Harness: "opencode", Version: version, Results: map[string]result{}}
	for k, v := range res {
		h.Results[k] = result{Status: v, Reason: "why " + k}
	}
	return Report{Schema: 1, Finished: time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC), Harnesses: []HarnessResult{h}}
}

func TestDiffReports(t *testing.T) {
	old := rep("1.4.2", map[string]string{"create": pass, "first_turn": pass, "approval": fail, "interrupt": pass, "slash": pass, "mode": skip})
	same := rep("1.4.2", map[string]string{"create": pass, "first_turn": fail, "approval": pass, "interrupt": interrupted, "slash": pass, "mode": skip})
	got := diffReports(old, same)
	if len(got) != 2 || got[0].Scenario != "first_turn" || !got[0].Regressed() || got[0].Upstream() || got[1].Scenario != "approval" || got[1].To != pass {
		t.Fatalf("same version: %+v", got)
	}
	var out bytes.Buffer
	if code := printDiff(&out, got); code != 1 || !strings.Contains(out.String(), "regressed") || !strings.Contains(out.String(), "fixed") {
		t.Fatalf("code %d, output:\n%s", code, out.String())
	}

	newer := rep("1.5.0", map[string]string{"create": pass, "first_turn": pass, "approval": fail, "interrupt": pass, "slash": fail, "mode": skip})
	got = diffReports(old, newer)
	if len(got) != 1 || !got[0].Upstream() || !got[0].Regressed() {
		t.Fatalf("new version: %+v", got)
	}
	out.Reset()
	if code := printDiff(&out, got); code != 1 || !strings.Contains(out.String(), "agent updated") || !strings.Contains(out.String(), "1.4.2 to 1.5.0") {
		t.Fatalf("code %d, output:\n%s", code, out.String())
	}

	// An agent that was not in the old run, and a run with nothing changed.
	fresh := rep("1.0", map[string]string{"create": fail})
	fresh.Harnesses[0].Harness = "pi"
	if got := diffReports(old, fresh); len(got) != 0 {
		t.Fatalf("a newly installed agent produced changes: %+v", got)
	}
	out.Reset()
	if code := printDiff(&out, diffReports(old, old)); code != 0 || strings.TrimSpace(out.String()) != "no change" {
		t.Fatalf("code %d, output %q", code, out.String())
	}
}

func TestPublicSummaryHasNoFailures(t *testing.T) {
	r := rep("1.4.2", map[string]string{"create": pass, "first_turn": pass, "approval": fail, "interrupt": interrupted, "mode": skip})
	r.Model = "vendor/small"
	r.Harnesses[0].Notes = []string{"a note"}
	bad := HarnessResult{Harness: "vibe", Version: "3", Results: map[string]result{"create": {fail, "x"}}}
	r.Harnesses = append(r.Harnesses, bad)
	sum := publicSummary(r)
	b, _ := json.Marshal(sum)
	for _, word := range []string{fail, skip, interrupted, "why", "a note", "vendor/small", "vibe", "approval", "reason"} {
		if bytes.Contains(b, []byte(word)) {
			t.Fatalf("public summary contains %q: %s", word, b)
		}
	}
	if len(sum.Agents) != 1 || strings.Join(sum.Agents[0].Checked, ",") != "create,first_turn" || sum.Agents[0].Date != "2026-11-18" || sum.Date != "2026-11-18" {
		t.Fatalf("summary = %+v", sum)
	}
}

func TestPublicSummaryOrder(t *testing.T) {
	var r Report
	for _, id := range []string{"claude", "pi", "aider", "omp", "codex", "opencode"} {
		r.Harnesses = append(r.Harnesses, HarnessResult{Harness: id, Results: map[string]result{"create": {pass, ""}}})
	}
	var ids []string
	for _, a := range publicSummary(r).Agents {
		ids = append(ids, a.ID)
	}
	if got := strings.Join(ids, " "); got != "opencode omp pi aider claude codex" {
		t.Fatalf("order = %s", got)
	}
}

func TestMergePublic(t *testing.T) {
	cur := PublicSummary{Schema: 1, Date: "2026-11-18", Agents: []PublicAgent{{ID: "opencode", Version: "1.5", Date: "2026-11-18", Checked: []string{"create"}}}}
	prev := PublicSummary{Schema: 1, Date: "2026-11-17", Agents: []PublicAgent{
		{ID: "opencode", Version: "1.4", Date: "2026-11-17", Checked: []string{"create", "first_turn"}},
		{ID: "codex", Version: "0.169", Date: "2026-11-15", Checked: []string{"first_turn", "create", "made_up"}},
		{ID: "cursor", Version: "1", Date: "2026-11-10", Checked: []string{"create"}},
		{ID: "pi", Version: "2", Date: "2026-11-17", Checked: []string{"create"}},
	}}
	// pi ran this time and nothing of it passed: its old entry must not come back.
	got := mergePublic(cur, prev, []string{"opencode", "pi"})
	if len(got.Agents) != 2 {
		t.Fatalf("agents = %+v", got.Agents)
	}
	if got.Agents[0].ID != "opencode" || got.Agents[0].Version != "1.5" || len(got.Agents[0].Checked) != 1 {
		t.Fatalf("this run's entry was replaced by the older one: %+v", got.Agents[0])
	}
	codex := got.Agents[1]
	if codex.ID != "codex" || codex.Date != "2026-11-15" || strings.Join(codex.Checked, ",") != "create,first_turn" {
		t.Fatalf("kept entry = %+v", codex)
	}
}

func TestPublishWritesWholeFile(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "r.json"), filepath.Join(dir, "latest.json")
	if err := rep("1.4.2", map[string]string{"create": pass}).write(in); err != nil {
		t.Fatal(err)
	}
	// A previous file that is not JSON is ignored, not fatal.
	prev := filepath.Join(dir, "prev.json")
	_ = os.WriteFile(prev, []byte("{half"), 0o644)
	if err := publish(in, out, prev); err != nil {
		t.Fatal(err)
	}
	var sum PublicSummary
	b, _ := os.ReadFile(out)
	if err := json.Unmarshal(b, &sum); err != nil || len(sum.Agents) != 1 {
		t.Fatalf("%v: %s", err, b)
	}
	if _, err := os.Stat(out + ".tmp"); err == nil {
		t.Fatal("the temporary file was left behind")
	}
}

func TestNoRouteBlocksAnAgentNotAllowedItsOwnAccount(t *testing.T) {
	router := &openRouter{model: "vendor/small"}
	strict := &options{router: router, ownOnly: true, ownOK: map[string]bool{"codex": true}}
	for id, wantBlocked := range map[string]bool{"pi": true, "codex": false, "goose": false} {
		r := &runner{id: id, opts: strict}
		r.noRoute("the model picker lists no vendor/small")
		if (r.blocked != "") != wantBlocked {
			t.Errorf("%s: blocked = %q", id, r.blocked)
		}
		if id == "goose" && r.ownAccount {
			t.Error("goose is wired through its environment: it is not on its own account")
		}
	}
	// Without -own-account every agent may, as before.
	r := &runner{id: "pi", opts: &options{router: router}}
	r.noRoute("x")
	if r.blocked != "" || !r.ownAccount {
		t.Fatalf("blocked %q, own %v", r.blocked, r.ownAccount)
	}
	// Without -openrouter there is nothing to route.
	r = &runner{id: "pi", opts: &options{ownOnly: true}}
	r.noRoute("x")
	if r.blocked != "" || r.ownAccount {
		t.Fatalf("blocked %q, own %v", r.blocked, r.ownAccount)
	}
}

func TestAvoidedModelsAreNeverPicked(t *testing.T) {
	o := &options{avoid: []string{"qwen", "local/"}}
	got := o.allowed([]pickerItem{{ID: "qwen5090/qwen3.8-27b"}, {ID: "kimi-code/k3"}, {ID: "x", Group: "Local/llama"}, {ID: "small", Title: "Qwen small"}})
	if len(got) != 1 || got[0].ID != "kimi-code/k3" {
		t.Fatalf("allowed = %+v", got)
	}
	if all := (&options{}).allowed([]pickerItem{{ID: "qwen"}}); len(all) != 1 {
		t.Fatal("with nothing to avoid every model stays")
	}
}

// An adapter with no model control cannot be put on the test model: its
// turns would run on whatever its own config names.
func TestAnAgentWithNoModelControlHasNoRoute(t *testing.T) {
	r := &runner{id: "vibe", opts: &options{router: &openRouter{model: "m"}, ownOnly: true, ownOK: map[string]bool{}}}
	r.modelSetup(context.Background())
	if r.blocked == "" || !r.ownAccount {
		t.Fatalf("blocked %q, own %v", r.blocked, r.ownAccount)
	}
}
