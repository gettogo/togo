package rpc

import (
	"encoding/json"
	"sync"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// threads is what a feed that follows every loaded thread knows of them on
// one connection: which threads the server had loaded when the feed first
// asked, which are top-level conversations, and the one the feed takes the
// terminal to show.
//
// A conversation resumed at the desk reaches the feed in one of two ways:
//
//   - The server had not loaded it: it appears in thread/loaded/list
//     within one poll, before any prompt. Its resume result goes out
//     marked late (joinRecord), which the mapper takes as the terminal
//     having moved there.
//   - The server still had it loaded (a conversation shown earlier in
//     this run, which the feed's own subscription keeps loaded): nothing
//     is sent before its next turn starts. Then the feed joins it again,
//     so the chat that starts over on it gets its earlier turns too.
//
// A late thread is one this session's own server loaded, so it is this
// session's terminal (or the phone through it) that opened it. Checked on
// Codex 0.170.0 with two servers started as the helper starts them
// (`app-server --listen unix://<session>.sock`, while a per-user Codex
// daemon served other terminals): each socket is a link to its own socket
// under /tmp/codex-daemon-<uid>/, held by its own server process; each
// server's thread/loaded/list held only its own threads, and a thread
// started on one was neither listed by nor announced to the other. Only
// thread/list (the conversations saved on disk) is shared, and the feed
// does not follow it.
//
// Set: on the first answer of thread/loaded/list, on each join. Cleared:
// with the connection (a reconnect or a helper restart asks again, and
// nothing then is late).
type threads struct {
	mu      sync.Mutex
	initial map[string]bool // the first listing; nil until the server answered
	top     map[string]bool
	cur     string // the top-level thread taken to be on the terminal
}

func newThreads() *threads { return &threads{top: map[string]bool{}} }

// listed records the server's first answer.
func (t *threads) listed(ids []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.initial != nil {
		return
	}
	t.initial = make(map[string]bool, len(ids))
	for _, id := range ids {
		t.initial[id] = true
	}
}

// late reports whether thread id was loaded after the feed first asked.
func (t *threads) late(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.initial != nil && !t.initial[id]
}

// joined records a resume result. The first top-level thread, or one
// loaded late, is taken to be the one on the terminal.
func (t *threads) joined(res json.RawMessage, late bool) {
	var r struct {
		Thread struct {
			ID        string  `json:"id"`
			Parent    *string `json:"parentThreadId"`
			Ephemeral bool    `json:"ephemeral"`
		} `json:"thread"`
	}
	if json.Unmarshal(res, &r) != nil || r.Thread.ID == "" || r.Thread.Ephemeral || (r.Thread.Parent != nil && *r.Thread.Parent != "") {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.top) < 4096 {
		t.top[r.Thread.ID] = true
	}
	if t.cur == "" || late {
		t.cur = r.Thread.ID
	}
}

// turnStarted returns the thread to join again when a turn starts on a
// top-level thread other than the one taken to be on the terminal and the
// one the mapper follows (followed, asked only then): the desk resumed a
// conversation the server still had loaded.
func (t *threads) turnStarted(params json.RawMessage, followed func() string) string {
	var p struct {
		ThreadID string `json:"threadId"`
	}
	if json.Unmarshal(params, &p) != nil || p.ThreadID == "" {
		return ""
	}
	t.mu.Lock()
	if !t.top[p.ThreadID] || p.ThreadID == t.cur {
		t.mu.Unlock()
		return ""
	}
	t.cur = p.ThreadID
	t.mu.Unlock()
	if followed() == p.ThreadID {
		return ""
	}
	return p.ThreadID
}

// again names the thread a turn/started record asks the feed to join
// again (threads.turnStarted), or "".
func (r *Runner) again(tr *threads, m Message, env harness.Env) string {
	if tr == nil || m.Method != "turn/started" {
		return ""
	}
	return tr.turnStarted(m.Params, func() string { return source.LoadFollowed(env, FollowedThread) })
}

// joinRecord is a thread/resume result as it enters the feed, marked late
// when the thread was loaded after the feed first asked.
func joinRecord(res json.RawMessage, late bool) Message {
	m := map[string]json.RawMessage{"result": res}
	if late {
		m["late"] = json.RawMessage("true")
	}
	params, _ := json.Marshal(m)
	return Message{Method: MethodResume, Params: params}
}
