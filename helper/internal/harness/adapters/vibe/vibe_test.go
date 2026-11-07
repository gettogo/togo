package vibe

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
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

// The fixture follows Vibe 3.1's LLMMessage model; no live journal exists.
func TestSessionJournal(t *testing.T) {
	m := newSession(harness.Env{SessionID: "s_vibe000001"})
	defer hookstyle.UnmarkLive("s_vibe000001")
	f, err := os.Open(filepath.Join("testdata", "messages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	final := map[string]harness.Item{}
	var order []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		items, _ := m.Map(harness.Record{At: time.Unix(1, 0), Data: json.RawMessage(sc.Text())})
		for _, x := range items {
			if _, seen := final[x.ID]; !seen {
				order = append(order, x.ID)
			}
			final[x.ID] = x
		}
	}
	if u := final["m1"].User; u == nil || u.Text != "check the tree" {
		t.Fatalf("user %+v", u)
	}
	c := final["call_1"].Tool
	if c == nil || c.Title != "git status" || c.State != harness.StateOK || c.Output != "On branch main" {
		t.Fatalf("call %+v", c)
	}
	if r := final["m3:text"].Assistant; r == nil || r.Text != "The tree is clean." {
		t.Fatalf("reply %+v", r)
	}
	if tb := final["m1.turn"].Turn; tb == nil || tb.State != harness.StateOK || tb.Steps != 1 {
		t.Fatalf("turn %+v", tb)
	}
	if _, ok := final["m4"]; ok {
		t.Fatal("an injected message showed as a prompt")
	}
	if n := final["m5"].Notice; n == nil || n.Code != harness.NoticeCompacted {
		t.Fatalf("compaction %+v", n)
	}
	if final["m2:thought"].Thinking == nil {
		t.Fatal("reasoning missing")
	}
}

func TestJournalForPicksThisFolder(t *testing.T) {
	home := t.TempDir()
	write := func(dir, cwd string, age time.Duration) string {
		p := filepath.Join(home, ".vibe", "logs", "session", dir)
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		meta := `{"session_id":"` + dir + `","environment":{"working_directory":"` + cwd + `"}}`
		os.WriteFile(filepath.Join(p, "meta.json"), []byte(meta), 0o600)
		j := filepath.Join(p, "messages.jsonl")
		os.WriteFile(j, []byte("{}\n"), 0o600)
		at := time.Now().Add(-age)
		os.Chtimes(j, at, at)
		return j
	}
	want := write("a", "/w/proj", time.Minute)
	write("b", "/w/other", 0)
	write("c", "/w/proj", 2*time.Hour)
	env := harness.Env{Cwd: "/w/proj", Vars: map[string]string{source.VarHome: home}}
	if got := journalFor(env); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
