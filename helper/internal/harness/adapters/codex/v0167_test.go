package codex

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// The adapter's version range admits the Codex the 0.167 recordings came
// from and still the 0.144 line it was first checked on.
func TestVersionRangeCoversRecorded(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	r := a.Harness.Range
	if !strings.Contains(r, ">=0.144") || !strings.Contains(r, "<0.168") {
		t.Fatalf("range %q does not cover 0.144 to 0.167", r)
	}
}

// A real Codex 0.167.0 session, joined as a second client: a resumed
// paginated thread with a finished turn, Codex's ephemeral title thread
// starting beside it, and a new turn with a file approval answered at the
// terminal.
func TestRecorded0167(t *testing.T) {
	m := NewAppServer(harness.Env{SessionID: "s_0167", Cwd: "/Users/me/proj"})
	out := run(m, records(t, "appserver-0167.jsonl"))
	golden(t, "appserver-0167.golden.json", map[string]any{"items": out.items, "ops": out.ops, "state": m.State()})
	st := m.State()
	if st["harness_session"] != "01a1bbac-a0e4-7321-9ea5-c9ceabfb4575" {
		t.Fatalf("the title thread took over the session: %v", st)
	}
	if st["model"] != "gpt-6.1-sol" || st["effort"] != "low" || st["mode"] != "on-request" || st["working"] != "false" {
		t.Fatalf("state %v", st)
	}
	items := out.last()
	if x := items["exec-e5f88f04-20ee-40f6-810f-f3275c632fb0"]; x.Tool == nil || x.Tool.Title != "Read main.go" {
		t.Fatalf("history read %+v", x)
	}
	if x := items["exec-62f254d5-6f3d-4ad3-9a08-dd186fa6eb56"]; x.Tool == nil || x.Tool.Title != "List ." || x.Tool.State != harness.StateOK {
		t.Fatalf("list %+v", x.Tool)
	}
	var waited bool
	for _, x := range out.items {
		if x.ID == "exec-78477322-19e8-4862-9621-a6f136bb2789" && x.Tool != nil && x.Tool.State == harness.StateWaiting {
			waited = true
		}
	}
	fc := items["exec-78477322-19e8-4862-9621-a6f136bb2789"]
	if !waited || fc.Tool == nil || fc.Tool.Title != "Write notes.txt" || fc.Tool.State != harness.StateOK || len(fc.Tool.Files) != 1 || fc.Tool.Files[0].Path != "notes.txt" {
		t.Fatalf("file change (waited %v) %+v", waited, fc.Tool)
	}
	turn := items["01a1bbae-0f5e-7d41-80f5-b3a05ed572ee"]
	if turn.Turn == nil || turn.Turn.State != harness.StateOK || turn.Turn.FilesChanged != 1 {
		t.Fatalf("turn %+v", turn.Turn)
	}
}

// Item types and error codes that Codex 0.150 to 0.167 added.
func TestItems0167(t *testing.T) {
	m := NewAppServer(harness.Env{SessionID: "s_0167_items", Cwd: "/Users/me/proj"})
	out := run(m, records(t, "appserver-0167-items.jsonl"))
	items := out.last()

	fco := items["fco_1"]
	if fco.Tool == nil || fco.Tool.Title != "tracker: lookup_ticket" || fco.Tool.State != harness.StateOK || fco.Tool.Output != "Ticket 42 is open." {
		t.Fatalf("function call output %+v", fco.Tool)
	}
	if x := items["fco_2"]; x.Tool == nil || x.Tool.Title != "render" || x.Tool.Output != "rendered" {
		t.Fatalf("function call output with content items %+v", x.Tool)
	}

	msg := items["msg_async"]
	if msg.Assistant == nil {
		t.Fatalf("async message %+v", msg)
	}
	for _, want := range []string{"I can keep going two ways.", "Which database?", "Postgres", "SQLite", "Anything else?"} {
		if !strings.Contains(msg.Assistant.Text, want) {
			t.Fatalf("async questions missing %q: %q", want, msg.Assistant.Text)
		}
	}

	if e := items["error-019b0000-0000-7000-8000-0000000000t1"]; e.Notice == nil || e.Notice.Reason != harness.ErrRefused {
		t.Fatalf("cyber policy notice %+v", e.Notice)
	}
	e := items["error-019b0000-0000-7000-8000-0000000000t2"]
	if e.Notice == nil || e.Notice.Reason != harness.ErrRefused || !strings.Contains(e.Notice.Text, "operator policy") {
		t.Fatalf("misalignment notice %+v", e.Notice)
	}
}

// Codex 0.167 refuses to load a config.toml whose approval_policy is
// "untrusted" ("no longer supported; remove this setting"), so the mode
// picker, which writes the choice to config.toml, never offers it.
func TestModesNeverWriteUntrusted(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if a.Modes == nil || len(a.Modes.Choices) == 0 {
		t.Fatal("no mode choices")
	}
	for _, c := range a.Modes.Choices {
		if c.ID == "untrusted" {
			t.Fatalf("mode choice %+v would break Codex 0.167's config", c)
		}
	}
}
