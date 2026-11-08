package crush

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func feedFile(t *testing.T, m *db, name string, final map[string]harness.Item) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		feedRow(m, sc.Text(), final)
	}
}

func feedRow(m *db, row string, final map[string]harness.Item) {
	items, _ := m.Map(harness.Record{At: time.Unix(1, 0), Data: json.RawMessage(row)})
	for _, x := range items {
		final[x.ID] = x
	}
}

const sid = "e78ad987-bdbc-4d2a-9165-4bf1ae2db096"

// The fixtures are the rows of a real Crush 0.100.0 crush.db after one
// `crush run` turn (a view call, then the reply), paths scrubbed.
func TestDBRows(t *testing.T) {
	m := newDB(harness.Env{Cwd: "/w/proj"})
	final := map[string]harness.Item{}
	feedFile(t, m, "db-sessions.jsonl", final)
	if s := m.State()["harness_session"]; s != sid {
		t.Fatalf("session %q", s)
	}
	feedFile(t, m, "db-messages.jsonl", final)
	if u := final["7ad6aa87-bc76-4e04-9873-cb5d973a8b44"].User; u == nil || u.Text != "Use your view tool to read a.txt, then reply with only its contents." {
		t.Fatalf("user %+v", u)
	}
	c := final["call_2ae03632cf8243179c14b87f"].Tool
	if c == nil || c.Name != "view" || c.State != harness.StateOK || c.Output == "" {
		t.Fatalf("call %+v", c)
	}
	if r := final["6a775999-437f-4f1c-a7af-ef9d19134fd3:text"].Assistant; r == nil || r.Text != "hello" {
		t.Fatalf("reply %+v", r)
	}
	if final["1f85652c-2548-4ded-b171-49800692626f:thought"].Thinking == nil {
		t.Fatal("reasoning missing")
	}
	tb := final["7ad6aa87-bc76-4e04-9873-cb5d973a8b44.turn"].Turn
	if tb == nil || tb.State != harness.StateOK || tb.Steps != 1 {
		t.Fatalf("turn %+v", tb)
	}
	if m.State()["working"] != "false" {
		t.Fatal("still working after end_turn")
	}
}

// A row rewritten while it streams sends the same ids again, growing.
func TestDBStreamingRewrite(t *testing.T) {
	m := newDB(harness.Env{})
	final := map[string]harness.Item{}
	feedRow(m, `{"id":"s1","title":"t","created_at":100,"updated_at":100}`, final)
	feedRow(m, `{"id":"u1","session_id":"s1","role":"user","parts":"[{\"type\":\"text\",\"data\":{\"text\":\"hi\"}}]","created_at":100,"updated_at":100}`, final)
	feedRow(m, `{"id":"a1","session_id":"s1","role":"assistant","parts":"[{\"type\":\"text\",\"data\":{\"text\":\"Hel\"}}]","created_at":100,"updated_at":101}`, final)
	if r := final["a1:text"].Assistant; r == nil || r.Text != "Hel" || final["u1.turn"].Turn.State != harness.StateRunning {
		t.Fatalf("partial %+v", final)
	}
	feedRow(m, `{"id":"a1","session_id":"s1","role":"assistant","parts":"[{\"type\":\"text\",\"data\":{\"text\":\"Hello\"}},{\"type\":\"finish\",\"data\":{\"reason\":\"end_turn\"}}]","created_at":100,"updated_at":102,"finished_at":102}`, final)
	if r := final["a1:text"].Assistant; r.Text != "Hello" || final["u1.turn"].Turn.State != harness.StateOK {
		t.Fatalf("whole %+v", final)
	}
	// Rows of another session are not this chat.
	feedRow(m, `{"id":"u9","session_id":"s9","role":"user","parts":"[{\"type\":\"text\",\"data\":{\"text\":\"x\"}}]","created_at":100,"updated_at":100}`, final)
	if _, ok := final["u9"]; ok {
		t.Fatal("another session's prompt shown")
	}
}
