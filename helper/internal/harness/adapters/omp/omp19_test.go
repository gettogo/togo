package omp

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// session-19.jsonl is a real omp 19.0.1 session (two turns: a bash call,
// then a one-word reply), scrubbed. 19 adds a runtime version to every
// record, model_usage records for the composer's next-message prediction,
// and the model opened its tool call with a bare "\n\n" text block.
func TestOmp19SessionMaps(t *testing.T) {
	m := newSession(env())
	recs := records(t, "session-19.jsonl", "session")
	out := run(m, recs)
	var users, replies, turns []string
	tools := 0
	for _, it := range out.last() {
		switch it.Kind {
		case harness.KindUser:
			users = append(users, it.User.Text)
		case harness.KindAssistant:
			if strings.TrimSpace(it.Assistant.Text) == "" {
				t.Errorf("blank reply item %s", it.ID)
			}
			replies = append(replies, it.Assistant.Text)
		case harness.KindTool:
			tools++
			if it.Tool.State != harness.StateOK {
				t.Errorf("tool %s state %q", it.Tool.Name, it.Tool.State)
			}
		case harness.KindTurn:
			turns = append(turns, it.Turn.State)
		}
	}
	if len(users) != 2 || len(replies) != 2 || tools != 1 || len(turns) != 2 {
		t.Fatalf("users %q, replies %q, tools %d, turns %q", users, replies, tools, turns)
	}
	for _, s := range turns {
		if s != harness.StateOK {
			t.Errorf("turn state %q", s)
		}
	}
	st := m.State()
	if st["model"] != "openrouter/deepseek/deepseek-v4-flash" || st["effort"] != "high" || st["working"] != "false" {
		t.Errorf("state model %q effort %q working %q", st["model"], st["effort"], st["working"])
	}
	// The spend matches omp's own: every request, the predictions too.
	want := 0.0
	for _, r := range recs {
		var e struct {
			Type    string `json:"type"`
			Usage   usage  `json:"usage"`
			Message struct {
				Role  string `json:"role"`
				Usage usage  `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(r.Data, &e); err != nil {
			t.Fatal(err)
		}
		want += e.Usage.Cost.Total + e.Message.Usage.Cost.Total
	}
	var got float64
	for _, op := range out.Ops {
		if op.Status != nil && op.Status.CostUSD != nil {
			got = *op.Status.CostUSD
		}
	}
	if want == 0 || math.Abs(got-want) > 1e-12 {
		t.Errorf("cost %v, want %v", got, want)
	}
}

type usage struct {
	Cost struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

// A replay of the same file counts each request once.
func TestOmp19ReplayCountsSpendOnce(t *testing.T) {
	cost := func(m *session, batches ...[]harness.Record) float64 {
		var got float64
		for _, op := range run(m, batches...).Ops {
			if op.Status != nil && op.Status.CostUSD != nil {
				got = *op.Status.CostUSD
			}
		}
		return got
	}
	recs := records(t, "session-19.jsonl", "session")
	once := cost(newSession(env()), recs)
	twice := cost(newSession(env()), recs, recs)
	if once == 0 || math.Abs(once-twice) > 1e-12 {
		t.Fatalf("once %v, twice %v", once, twice)
	}
}

// omp 19's `omp ps --json` (ps-19.json, real): one daemon per user and
// {daemon, reports: [{scope, units}]}. The only unit there is omp's own word
// completion, which is not the agent's work.
func TestJobsReadOmp19Reports(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	sel := a.Sources["jobs"].Select
	var real any
	if err := json.Unmarshal(mustRead(t, "ps-19.json"), &real); err != nil {
		t.Fatal(err)
	}
	out, err := engine.JQ(sel, real, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rows := out[0].([]any); len(rows) != 0 {
		t.Fatalf("rows %v, want omp's own prediction unit left out", rows)
	}
	// A unit the agent started, in the shape the real report gives units.
	var in any
	json.Unmarshal([]byte(`{"daemon":{"pid":1},"reports":[{"scope":{"kind":"project","dir":"/Users/dev/proj"},"units":[
		{"id":"5f0c2a8e-1111-4c1e-9a55-2d6f4f3b9e01","kind":"service","scope":{"kind":"project","dir":"/Users/dev/proj"},
		 "key":"9a1b","label":"dev","command":"npm run dev","state":"running"}]}]}`), &in)
	out, err = engine.JQ(sel, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := out[0].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows %v", rows)
	}
	row := rows[0].(map[string]any)
	if row["rowId"] != "5f0c2a8e-1111-4c1e-9a55-2d6f4f3b9e01" || row["name"] != "dev" {
		t.Fatalf("row %v", row)
	}
	if sub, _ := engine.MapValue(a.Mission["background"].Map["item.subtitle"], row, nil); sub != "npm run dev" {
		t.Errorf("subtitle %q", sub)
	}
	stop := a.Actions["stop"].Branch
	if c, _ := engine.Render(stop[0].When, map[string]any{"target": row["rowId"]}); c != "" {
		t.Fatal("a 19 unit took the 18 global branch")
	}
	var argv []string
	for _, x := range stop[1].CLI.Argv {
		r, _ := engine.Render(x, map[string]any{"target": row["rowId"]})
		argv = append(argv, r)
	}
	if want := "omp ps stop 5f0c2a8e-1111-4c1e-9a55-2d6f4f3b9e01"; strings.Join(argv, " ") != want {
		t.Errorf("argv %q, want %q", strings.Join(argv, " "), want)
	}
}

// Seen live on omp 19.0.1: --smol was carried as a flag, so a resume ran
// `omp --resume <id> --smol --config <file>`; omp took --config as the
// model and sent the file's path as a prompt.
func TestResumeCarriesSmolWithItsModel(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	r := a.Actions["resume"].Relaunch
	running := []string{"omp", "--model", "openrouter/x", "--smol", "openrouter/y", "--config", "/Users/dev/o.yml", "--slow=openrouter/z", "--no-title"}
	next := []string{"omp", "--resume", "01a1f71a"}
	got := strings.Join(r.Carry.Pick(running, next, 4096), " ")
	if want := "--model openrouter/x --smol openrouter/y --config /Users/dev/o.yml --slow=openrouter/z --no-title"; got != want {
		t.Fatalf("carried %q, want %q", got, want)
	}
}
