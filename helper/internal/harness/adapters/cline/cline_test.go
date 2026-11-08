package cline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "cline" {
		t.Fatalf("adapter %q", a.Adapter)
	}
}

const fixtureID = "1794109072649_04an1"

// home lays the recorded session (Cline 3.0.74, one turn against a local
// model, paths scrubbed) out as ~/.cline/data/sessions/<id>/.
func home(t *testing.T) (string, []byte) {
	t.Helper()
	h := t.TempDir()
	dir := filepath.Join(h, ".cline", "data", "sessions", fixtureID)
	_ = os.MkdirAll(dir, 0o755)
	msgs, err := os.ReadFile("testdata/session.messages.json")
	if err != nil {
		t.Fatal(err)
	}
	man, _ := os.ReadFile("testdata/session.json")
	_ = os.WriteFile(filepath.Join(dir, fixtureID+".messages.json"), msgs, 0o600)
	_ = os.WriteFile(filepath.Join(dir, fixtureID+".json"), man, 0o600)
	return h, msgs
}

func env(h, created string, argv ...string) harness.Env {
	return harness.Env{Cwd: "/Users/me/proj", Argv: argv, Vars: map[string]string{source.VarHome: h, "created": created}}
}

func TestMessagesMapsTheRecordedTurn(t *testing.T) {
	h, doc := home(t)
	m := newMessages(env(h, "2026-11-08T03:37:52Z"))
	items, _ := m.Map(harness.Record{Source: "sessions", Data: doc})
	var desc []string
	for _, it := range items {
		switch it.Kind {
		case harness.KindUser:
			desc = append(desc, "user:"+it.User.Text)
		case harness.KindThinking:
			desc = append(desc, "thinking")
		case harness.KindAssistant:
			desc = append(desc, "assistant:"+it.Assistant.Text)
		case harness.KindTool:
			desc = append(desc, "tool:"+it.Tool.Category+":"+it.Tool.Title+":"+it.Tool.State+":"+it.Tool.Output)
		case harness.KindTurn:
			desc = append(desc, "turn:"+it.Turn.State+":"+strconv.Itoa(it.Turn.Steps))
		}
	}
	want := []string{
		"user:Create a file named hi.txt containing the word hello, then reply with one word: done",
		"thinking",
		"tool:write:Write /Users/me/proj/hi.txt:ok:File created successfully at: /Users/me/proj/hi.txt",
		"thinking",
		"assistant:done",
		"turn:ok:1",
	}
	if strings.Join(desc, "|") != strings.Join(want, "|") {
		t.Fatalf("items:\n%q\nwant:\n%q", desc, want)
	}
	st := m.State()
	if st["harness_session"] != fixtureID || st["mode"] != "act" || st["model"] != "coder-fast" ||
		st["transcript_path"] != filepath.Join(h, ".cline", "data", "sessions", fixtureID, fixtureID+".messages.json") {
		t.Fatalf("state = %v", st)
	}
	again, _ := m.Map(harness.Record{Source: "messages", Data: doc})
	if len(again) != 0 {
		t.Fatalf("an unchanged file sent %d items again", len(again))
	}
}

// A session that started before this one, in another folder, or a
// subagent's file is not followed; a --id launch follows its id at once.
func TestMessagesPicksOnlyThisSession(t *testing.T) {
	h, doc := home(t)
	if items, _ := newMessages(env(h, "2026-11-08T04:00:00Z")).Map(harness.Record{Data: doc}); len(items) != 0 {
		t.Fatal("an older session was followed")
	}
	other := env(h, "2026-11-08T03:37:52Z")
	other.Cwd = "/Users/me/elsewhere"
	if items, _ := newMessages(other).Map(harness.Record{Data: doc}); len(items) != 0 {
		t.Fatal("another folder's session was followed")
	}
	sub := strings.Replace(string(doc), `"agent": "lead"`, `"agent": "subagent"`, 1)
	if items, _ := newMessages(env(h, "2026-11-08T03:37:52Z")).Map(harness.Record{Data: []byte(sub)}); len(items) != 0 {
		t.Fatal("a subagent file was followed")
	}
	m := newMessages(env(h, "2026-11-08T04:00:00Z", "cline", "-i", "--id", fixtureID))
	if m.State()["harness_session"] != fixtureID {
		t.Fatalf("state = %v", m.State())
	}
	if items, _ := m.Map(harness.Record{Data: doc}); len(items) == 0 {
		t.Fatal("the resumed session was not mapped")
	}
}

// The turn stays open while the last message is a tool call, and closes
// when the pane goes idle.
func TestMessagesIdleClosesAnOpenTurn(t *testing.T) {
	h, doc := home(t)
	var d map[string]any
	_ = json.Unmarshal(doc, &d)
	d["messages"] = d["messages"].([]any)[:2]
	cut, _ := json.Marshal(d)
	m := newMessages(env(h, "2026-11-08T03:37:52Z"))
	items, _ := m.Map(harness.Record{Data: cut})
	last := items[len(items)-1]
	if last.Kind != harness.KindTurn || last.Turn.State != harness.StateRunning {
		t.Fatalf("last = %+v", last)
	}
	closed := m.CloseIdle(time.Now())
	var turn, tool string
	for _, it := range closed {
		switch it.Kind {
		case harness.KindTurn:
			turn = it.Turn.State
		case harness.KindTool:
			tool = it.Tool.State
		}
	}
	if turn != harness.StateOK || tool != harness.StateInterrupted {
		t.Fatalf("closed: turn %q tool %q", turn, tool)
	}
	if m.CloseIdle(time.Now()) != nil {
		t.Fatal("closed twice")
	}
}

func TestUserTextUnwrapsTheEnvelope(t *testing.T) {
	if txt, mode := userText("<user_input mode=\"plan\">a\nb</user_input>"); txt != "a\nb" || mode != "plan" {
		t.Fatalf("%q %q", txt, mode)
	}
	if txt, mode := userText(" plain "); txt != "plain" || mode != "" {
		t.Fatalf("%q %q", txt, mode)
	}
}
