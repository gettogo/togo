package rpc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// joins counts thread/resume calls per thread and answers each with a
// top-level thread, or a subagent of thr-a for ids starting "sub".
type joins struct {
	mu sync.Mutex
	n  map[string]int
}

func (j *joins) handle(method string, params json.RawMessage) (any, *Error) {
	if method != MethodResume {
		return resumeHandler(method, params)
	}
	var p struct {
		ThreadID string `json:"threadId"`
	}
	json.Unmarshal(params, &p)
	j.mu.Lock()
	j.n[p.ThreadID]++
	j.mu.Unlock()
	var parent any
	if len(p.ThreadID) > 3 && p.ThreadID[:3] == "sub" {
		parent = "thr-a"
	}
	return map[string]any{"thread": map[string]any{"id": p.ThreadID, "parentThreadId": parent, "turns": []any{}}}, nil
}

func (j *joins) count(id string) int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.n[id]
}

// lates reports, per joined thread, whether its record was marked late.
func (r *recorder) lates() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]bool{}
	for _, m := range r.recs {
		if m.Method != MethodResume {
			continue
		}
		var p struct {
			Result struct {
				Thread struct {
					ID string `json:"id"`
				} `json:"thread"`
			} `json:"result"`
			Late bool `json:"late"`
		}
		json.Unmarshal(m.Params, &p)
		out[p.Result.Thread.ID] = out[p.Result.Thread.ID] || p.Late
	}
	return out
}

func turnStarted(thread string) string {
	return `{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"` + thread + `","turn":{"id":"t-` + thread + `","items":[],"status":"inProgress"}}}`
}

// A conversation the server loads after the feed first asked (a /resume
// or /new at the desk) is joined at once and marked late; the ones loaded
// when the feed connected (after a helper restart, a reconnect) are not.
// A turn starting on a top-level thread the server kept loaded, other
// than the one followed, makes the feed join it again.
func TestFeedMarksLateJoinsAndJoinsAResumedThreadAgain(t *testing.T) {
	sock := filepath.Join(socketDir(t), "s.sock")
	f := startFake(t, sock)
	j := &joins{n: map[string]int{}}
	f.handle = j.handle
	f.loaded = []string{"thr-a"}
	env := harness.Env{SessionID: "s_rpcdesk1", Endpoint: "unix://" + sock, Vars: map[string]string{source.VarTogoHome: t.TempDir()}}
	r := NewRunner()
	r.Poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &recorder{}
	go r.Watch(ctx, feedSpec(t, spec.RPC{Method: MethodResume, Params: map[string]string{"threadId": ""}}), env, rec.emit)
	waitFor(t, "the first join", func() bool { return j.count("thr-a") == 1 })
	source.SaveFollowed(env, FollowedThread, "thr-a") // as the mapper does

	f.mu.Lock()
	f.loaded = []string{"thr-a", "thr-b", "sub-a1"}
	f.mu.Unlock()
	waitFor(t, "the late joins", func() bool {
		l := rec.lates()
		_, b := l["thr-b"]
		_, s := l["sub-a1"]
		return b && s
	})
	source.SaveFollowed(env, FollowedThread, "thr-b")
	if l := rec.lates(); l["thr-a"] || !l["thr-b"] || !l["sub-a1"] {
		t.Fatalf("late marks %v", l)
	}

	// /resume of thr-a, which the server kept loaded: its prompt.
	f.broadcast(turnStarted("thr-a"))
	waitFor(t, "thr-a joined again", func() bool { return j.count("thr-a") == 2 })
}

// Which turn starts make the feed join a thread again: only the first on
// a top-level thread that is neither the one taken to be on the terminal
// nor the one the mapper follows.
func TestThreadsJoinAgainOnlyOnAMove(t *testing.T) {
	tr := newThreads()
	tr.listed([]string{"thr-a"})
	res := func(id, parent string) json.RawMessage {
		p := "null"
		if parent != "" {
			p = `"` + parent + `"`
		}
		return json.RawMessage(`{"thread":{"id":"` + id + `","parentThreadId":` + p + `}}`)
	}
	tr.joined(res("thr-a", ""), tr.late("thr-a"))
	if tr.late("thr-a") || !tr.late("thr-b") {
		t.Fatal("late is wrong")
	}
	tr.joined(res("thr-b", ""), true) // the /new
	tr.joined(res("sub-a1", "thr-a"), true)
	tr.joined(json.RawMessage(`{"thread":{"id":"thr-title","parentThreadId":null,"ephemeral":true}}`), true)
	followed := "thr-b"
	ask := func(id string) string {
		return tr.turnStarted(json.RawMessage(`{"threadId":"`+id+`"}`), func() string { return followed })
	}
	for _, c := range []struct{ id, want string }{
		{"thr-b", ""},      // the one on the terminal
		{"sub-a1", ""},     // a subagent
		{"thr-title", ""},  // ephemeral, never top
		{"thr-x", ""},      // never joined
		{"thr-a", "thr-a"}, // the resume of a still loaded conversation
		{"thr-a", ""},      // its next turn
	} {
		if got := ask(c.id); got != c.want {
			t.Fatalf("turn on %s: join %q, want %q", c.id, got, c.want)
		}
	}
	// The mapper already follows the thread (it moved on a late join the
	// feed has not seen as a move): no second join.
	followed = "thr-b"
	if got := ask("thr-b"); got != "" {
		t.Fatalf("turn on the followed thread: join %q", got)
	}
	if got := tr.turnStarted(json.RawMessage(`{"threadId":`), func() string { return "" }); got != "" {
		t.Fatalf("torn params: join %q", got)
	}
}
