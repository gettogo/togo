package vibe

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Feeds["session"].Mapper != MapperSession || a.Feeds["hooks"].Mapper != MapperHooks {
		t.Fatalf("feeds %+v", a.Feeds)
	}
}

// replay maps a fixture and returns each item's last version, and the
// items in the order first seen.
func replay(t *testing.T, m *session, name string) (map[string]harness.Item, []harness.Item) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	final := map[string]harness.Item{}
	var order []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		items, _ := m.Map(harness.Record{At: time.Unix(1, 0), Data: json.RawMessage(sc.Text())})
		for _, x := range items {
			if _, seen := final[x.ID]; !seen {
				order = append(order, x.ID)
			}
			final[x.ID] = x
		}
	}
	out := make([]harness.Item, len(order))
	for i, id := range order {
		out[i] = final[id]
	}
	return final, out
}

func newTestSession(t *testing.T, home string) *session {
	t.Helper()
	id := "s_vibe" + t.Name()
	t.Cleanup(func() { hookstyle.UnmarkLive(id) })
	return newSession(harness.Env{SessionID: id, Vars: map[string]string{source.VarHome: home}})
}

const toolTurn = "turn-e7625568e7604aa7572a9af30a425266"

func checkToolTurn(t *testing.T, final map[string]harness.Item, order []harness.Item) {
	t.Helper()
	if u := final["user-"+toolTurn+"-1"].User; u == nil || u.Text != "Use the bash tool to run: cat a.txt. Then reply with just the file contents." {
		t.Fatalf("user %+v", u)
	}
	if final["reasoning-289ebba5-e9dc-58eb-9744-9f217ced5037-0"].Thinking == nil {
		t.Fatal("reasoning missing")
	}
	c := final["effect-daf926e9-e049-5c1f-879d-145c20ae62b2"].Tool
	if c == nil || c.Title != "cat a.txt" || c.Category != harness.ToolShell || c.State != harness.StateOK ||
		c.Output != "hello" || c.ExitCode == nil || *c.ExitCode != 0 || c.Ended == nil {
		t.Fatalf("call %+v", c)
	}
	if r := final["assistant-66a15e12-ab42-54a7-acf0-f24810a5cbc0"].Assistant; r == nil || r.Text != "hello" {
		t.Fatalf("reply %+v", r)
	}
	tb := final[toolTurn].Turn
	if tb == nil || tb.State != harness.StateOK || tb.Steps != 1 || tb.Ended == nil || tb.Ended.Sub(tb.Started) <= 0 {
		t.Fatalf("turn %+v", tb)
	}
	if order[0].Kind != harness.KindTurn || order[1].Kind != harness.KindUser {
		t.Fatalf("order %v %v", order[0].Kind, order[1].Kind)
	}
}

// From a live Vibe 3.1.0 run against a local model: a bash call, then a reply.
func TestSessionJournalTool(t *testing.T) {
	m := newTestSession(t, t.TempDir())
	final, order := replay(t, m, "journal-tool.jsonl")
	checkToolTurn(t, final, order)
	if st := m.State(); st["working"] != "false" || st["session_id"] != "c62e7a44-5a4f-1af0-5ad4-d1c9fa135f62" {
		t.Fatalf("state %v", st)
	}
}

// Live runs against a stub provider that answered 401, 404 and 400
// (context length), and one that answered 429 until the run was stopped.
func TestFailedTurns(t *testing.T) {
	for _, c := range []struct{ file, reason, lead string }{
		{"journal-auth.jsonl", harness.ErrAuth, "The provider rejected the sign-in or API key: Invalid API key."},
		{"journal-model.jsonl", harness.ErrModelNotFound, "The model was not found: Client error '404 Not Found'"},
		{"journal-context.jsonl", harness.ErrContextOverflow, "The conversation is too long for the model: compaction summary"},
	} {
		t.Run(c.file, func(t *testing.T) {
			_, order := replay(t, newTestSession(t, t.TempDir()), c.file)
			var turn, notice *harness.Item
			for i := range order {
				switch order[i].Kind {
				case harness.KindTurn:
					turn = &order[i]
				case harness.KindNotice:
					notice = &order[i]
				}
			}
			if turn == nil || turn.Turn.State != harness.StateFailed || turn.Turn.Ended == nil {
				t.Fatalf("turn %+v", turn)
			}
			n := notice.Notice
			if notice == nil || n.Code != harness.NoticeError || n.Reason != c.reason || n.Level != "error" ||
				len(n.Text) < len(c.lead) || n.Text[:len(c.lead)] != c.lead || notice.TurnID != turn.ID {
				t.Fatalf("notice %+v", notice)
			}
		})
	}
	_, order := replay(t, newTestSession(t, t.TempDir()), "journal-interrupted.jsonl")
	for _, x := range order {
		if x.Kind == harness.KindNotice {
			t.Fatalf("an interrupted turn has a notice %+v", x.Notice)
		}
		if x.Kind == harness.KindTurn && x.Turn.State != harness.StateInterrupted {
			t.Fatalf("turn %+v", x.Turn)
		}
	}
}

func TestFailNoticeReasons(t *testing.T) {
	for _, c := range []struct {
		e      turnError
		reason string
	}{
		{turnError{Code: "model_stream_failed", Message: "Client error '429 Too Many Requests'"}, harness.ErrRateLimit},
		{turnError{Code: "model_stream_failed", Message: "boom"}, harness.ErrOther},
		{turnError{Code: "model_context_window_exceeded", Message: "too long"}, harness.ErrContextOverflow},
	} {
		if n := failNotice(&c.e); n.Reason != c.reason {
			t.Fatalf("%+v: %+v", c.e, n)
		}
	}
	var e turnError
	e.Code, e.Message, e.Details.ExceptionType = "model_stream_failed", "All connection attempts failed", "ConnectError"
	if n := failNotice(&e); n.Reason != harness.ErrProviderUnreachable {
		t.Fatalf("%+v", n)
	}
	e = turnError{Code: "model_stream_failed", Message: "Server error"}
	e.Details.HTTPStatus = 503
	if n := failNotice(&e); n.Reason != harness.ErrProviderUnreachable {
		t.Fatalf("%+v", n)
	}
	if n := failNotice(nil); n.Reason != harness.ErrOther || n.Text != "The turn failed" {
		t.Fatalf("%+v", n)
	}
}

// A resumed session's earlier turns live in the snapshot, read once the
// journal names the session.
func TestHistoryFromSnapshot(t *testing.T) {
	home, _ := filepath.Abs(filepath.Join("testdata", "home"))
	m := newTestSession(t, home)
	rec := `{"type":"projection_delta","payload":{"delta":[{"op":"set_envelope","state":{"session":{"id":"c62e7a44-5a4f-1af0-5ad4-d1c9fa135f62"}}}]}}`
	items, _ := m.Map(harness.Record{Data: json.RawMessage(rec)})
	final := map[string]harness.Item{}
	var order []harness.Item
	for _, x := range items {
		if _, seen := final[x.ID]; !seen {
			order = append(order, x)
		}
		final[x.ID] = x
	}
	for i, x := range order {
		order[i] = final[x.ID]
	}
	checkToolTurn(t, final, order)
	if st := m.State(); st["model"] != "qwen5090" {
		t.Fatalf("state %v", st)
	}
}

func TestJournalForPicksThisFolder(t *testing.T) {
	home := t.TempDir()
	write := func(id, cwd string, age time.Duration, parent bool) string {
		p := filepath.Join(home, ".vibe", "logs", "session", "unified", id)
		if err := os.MkdirAll(filepath.Join(p, "journal"), 0o700); err != nil {
			t.Fatal(err)
		}
		par := "null"
		if parent {
			par = `"root"`
		}
		meta := `{"session_id":"` + id + `","parent_session_id":` + par + `,"environment":{"working_directory":"` + cwd + `"}}`
		os.WriteFile(filepath.Join(p, "meta.json"), []byte(meta), 0o600)
		j := filepath.Join(p, "journal", "0000000000000001.jsonl")
		os.WriteFile(j, []byte("{}\n"), 0o600)
		at := time.Now().Add(-age)
		os.Chtimes(j, at, at)
		return filepath.Join(p, "journal", "*.jsonl")
	}
	want := write("a", "/w/proj", time.Minute, false)
	write("b", "/w/other", 0, false)
	write("c", "/w/proj", 2*time.Hour, false)
	write("d", "/w/proj", 0, true)
	env := harness.Env{Cwd: "/w/proj", Vars: map[string]string{source.VarHome: home}}
	if got := journalFor(env); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// Vibe records its agent nowhere but the footer; the mode is read there.
func TestModeFromTheFooter(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	read := func(line string) string {
		for _, r := range a.Modes.Screen {
			if regexp.MustCompile(r.Line).MatchString(line) {
				return r.ID
			}
		}
		return ""
	}
	for line, want := range map[string]string{
		"──────────── Qwen3.8 27B (5090)[off] · accept edits ─": "accept-edits",
		"──── Devstral 2 · ask ─":                                "ask",
		"──── Devstral 2 · auto approve":                         "auto-approve",
		"> Should I ask · or not?":                               "",
	} {
		if got := read(line); got != want {
			t.Errorf("%q read as %q, want %q", line, got, want)
		}
	}
}
