package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/opencode"
	"github.com/gettogo/togo/helper/internal/wire"
)

// sseData reads the data lines of a recorded event stream.
func sseData(t *testing.T, path string) [][]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 4<<20)
	for sc.Scan() {
		if d, ok := bytes.CutPrefix(sc.Bytes(), []byte("data: ")); ok {
			out = append(out, append([]byte(nil), d...))
		}
	}
	return out
}

// OpenCode 2.2.5 reported its session idle while a permission waited (the
// demo recording: the pane went quiet behind the permission panel), and the
// phone heard "done" while the agent needed the user. Replayed through the
// real mapper: the stream up to the permission ask, then an execution end
// before any reply. While the permission waits, no done goes out and the
// session needs the user; once it is answered, the turn's end is a done.
func TestOpenPermissionHoldsTheDone(t *testing.T) {
	const id = "s_permdone1"
	ap := &fakeApprovals{}
	e, s := questionEngine(t, id, ap)
	done := make(chan string, 4)
	e.cfg.Done = func(_, title, _ string) { done <- title }
	m := opencode.NewEvents2(harness.Env{Cwd: "/w"})
	feed := func(raw []byte) {
		items, ops := m.Map(harness.Record{Source: "events", At: time.Now(), Data: raw})
		s.mu.Lock()
		s.publishLocked(items, ops)
		s.mu.Unlock()
	}
	asked := false
	var session string
	for _, raw := range sseData(t, "../adapters/opencode/testdata/v2/events.sse") {
		var ev struct {
			Type string `json:"type"`
			Data struct {
				SessionID string `json:"sessionID"`
			} `json:"data"`
		}
		_ = json.Unmarshal(raw, &ev)
		feed(raw)
		if ev.Type == "permission.asked" {
			asked, session = true, ev.Data.SessionID
			break
		}
	}
	if !asked {
		t.Fatal("the recording has no permission ask")
	}
	if !e.NeedsYou(id) {
		t.Fatal("an open permission does not need the user")
	}
	// The execution ends with the permission still open.
	feed([]byte(`{"type":"session.execution.succeeded","data":{"sessionID":"` + session + `"}}`))
	s.mu.Lock()
	s.doneLocked()
	s.mu.Unlock()
	select {
	case title := <-done:
		t.Fatalf("done %q while a permission waits", title)
	case <-time.After(100 * time.Millisecond):
	}
	if !e.NeedsYou(id) {
		t.Fatal("the session stopped needing the user before the permission was answered")
	}
	// Answered: the wait is over and a turn's end is a done again.
	s.mu.Lock()
	for qid := range s.needs.open {
		q := s.needs.open[qid]
		s.publishLocked([]harness.Item{{ID: qid, Kind: harness.KindQuestion, Question: &harness.QuestionBody{
			Questions: q.Questions, Answered: map[string][]string{"0": {"once"}}}}}, nil)
	}
	s.mu.Unlock()
	if e.NeedsYou(id) {
		t.Fatal("still needs the user after the answer")
	}
	s.mu.Lock()
	s.doneLocked()
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("no done after the permission was answered")
	}
}

// A pending approval (a prompt on screen, a hook's request) needs the user
// too, and holds the done the same way.
func TestPendingApprovalNeedsTheUser(t *testing.T) {
	const id = "s_permdone2"
	ap := &fakeApprovals{pending: []wire.Approval{{RequestID: "rq_1", Title: "Bash"}}}
	e, _ := questionEngine(t, id, ap)
	if !e.NeedsYou(id) {
		t.Fatal("a pending approval does not need the user")
	}
	ap.mu.Lock()
	ap.pending = nil
	ap.mu.Unlock()
	if e.NeedsYou(id) {
		t.Fatal("needs the user with nothing pending")
	}
}
