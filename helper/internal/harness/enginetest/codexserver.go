package enginetest

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// CodexServer stands in for `codex app-server --listen unix://SOCK`, the
// server Togo starts for a Codex session and joins as a second client of
// the terminal's (sessiongen/codex.go describes what the helper reads and
// sends). The engine starts it as it starts the real one: the rig's fake
// codex program, run as `codex app-server --listen unix://SOCK`, notes
// its socket and waits; this server then listens there, so the engine's
// health check, feed and calls reach it through the production path.
//
// It speaks JSON-RPC 2.0 over a WebSocket, one message per text frame.
// What Codex does is its journal (sessiongen.CodexJournal): every line
// appended is sent to the clients that follow its thread, as the real
// server sends a running turn's notifications, and folded into the
// threads a thread/resume returns. It answers the calls the helper makes
// and records each one; a server request it sent waits for one answer,
// from any client or from the desk (AnswerAtDesk), and the server then
// says serverRequest/resolved. It can be told to fail or slow calls, drop
// its connections, restart, and stop.
type CodexServer struct {
	journal string

	mu      sync.Mutex
	path    string
	ln      net.Listener
	srv     *http.Server
	conns   map[*codexConn]bool
	off     int64
	fold    *codexFold
	calls   []CodexCall
	replies []CodexCall
	fail    map[string]*codexFail
	slow    map[string]time.Duration
	hang    map[string]chan struct{}
	tail    chan struct{}
	tailed  sync.WaitGroup
	seq     int // connections accepted

	// OnInterrupt runs for each turn/interrupt, outside the lock; OnReply
	// for each answer to a pending request.
	OnInterrupt func(thread, turn string)
	OnReply     func(req codexRequest, result json.RawMessage)
}

// CodexCall is one call a client made, or one answer it gave to a server
// request (Method is then the request's method).
type CodexCall struct {
	Conn int
	// ID is the request an answer answers; empty for a call.
	ID     json.RawMessage
	Method string
	Params json.RawMessage
	At     time.Time
}

type codexFail struct {
	code    int
	message string
	left    int
}

type codexConn struct {
	n      int
	c      *websocket.Conn
	out    chan []byte
	subs   map[string]bool
	ready  bool // initialized
	closed chan struct{}
	once   sync.Once
}

func newCodexServer(home, cwd string) *CodexServer {
	return &CodexServer{journal: sessiongen.CodexJournal(home), conns: map[*codexConn]bool{},
		fold: newCodexFold(cwd, sessiongen.CodexPolicy), fail: map[string]*codexFail{}, slow: map[string]time.Duration{},
		hang: map[string]chan struct{}{}}
}

// Listen serves at path: the socket the engine gave the app-server. The
// journal written so far is the server's history.
func (s *CodexServer) Listen(path string) error {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: http.HandlerFunc(s.accept), ReadHeaderTimeout: 5 * time.Second}
	s.mu.Lock()
	s.path, s.ln, s.srv = path, ln, srv
	s.catchUpLocked()
	if s.tail == nil {
		s.tail = make(chan struct{})
		s.tailed.Add(1)
		go s.follow(s.tail)
	}
	s.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// Path is where it listens, "" when it does not.
func (s *CodexServer) Path() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

// Close stops listening and ends every connection, as the server process
// ending. The threads stay on disk (the journal).
func (s *CodexServer) Close() {
	s.mu.Lock()
	srv, path := s.srv, s.path
	s.srv, s.ln, s.path = nil, nil, ""
	conns := s.conns
	s.conns = map[*codexConn]bool{}
	s.mu.Unlock()
	for c := range conns {
		c.close()
	}
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
		_ = srv.Close()
		_ = os.Remove(path)
	}
}

// Stop is Close and the end of the journal's follower, at the test's end.
func (s *CodexServer) Stop() {
	s.Close()
	s.mu.Lock()
	tail := s.tail
	s.tail = nil
	for k, ch := range s.hang {
		close(ch)
		delete(s.hang, k)
	}
	s.mu.Unlock()
	if tail != nil {
		close(tail)
	}
	s.tailed.Wait()
}

// Restart is the app-server process restarting on the same socket: every
// connection ends, and the terminal's client joins its thread again, so
// that thread is loaded and nothing else.
func (s *CodexServer) Restart() error {
	s.mu.Lock()
	path, main := s.path, s.fold.main()
	s.mu.Unlock()
	s.Close()
	s.mu.Lock()
	s.fold.loaded = nil
	if main != "" {
		s.fold.load(main)
	}
	s.mu.Unlock()
	return s.Listen(path)
}

// Relaunched is a new app-server for a new run of the terminal client:
// it has loaded only the thread the terminal resumes (thread), or none,
// and the terminal then starts a new thread (Append its thread/started).
func (s *CodexServer) Relaunched(thread string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fold.loaded = nil
	if thread != "" && s.fold.threads[thread] != nil {
		s.fold.load(thread)
	}
}

// DropConns ends every connection and keeps listening: the clients see
// their connection drop and dial again.
func (s *CodexServer) DropConns() {
	s.mu.Lock()
	conns := s.conns
	s.conns = map[*codexConn]bool{}
	s.mu.Unlock()
	for c := range conns {
		c.close()
	}
}

// Conns is how many clients are connected and past initialize.
func (s *CodexServer) Conns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for c := range s.conns {
		if c.ready {
			n++
		}
	}
	return n
}

// Fail makes the next n calls of method answer a JSON-RPC error; n < 0
// fails them until Heal.
func (s *CodexServer) Fail(method string, code int, message string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail[method] = &codexFail{code: code, message: message, left: n}
}

// Slow makes calls of method wait d before they are answered.
func (s *CodexServer) Slow(method string, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.slow[method] = d
}

// Hang makes calls of method wait for Heal before they are answered.
func (s *CodexServer) Hang(method string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hang[method] == nil {
		s.hang[method] = make(chan struct{})
	}
}

// Heal ends every Fail, Slow and Hang.
func (s *CodexServer) Heal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail, s.slow = map[string]*codexFail{}, map[string]time.Duration{}
	for k, ch := range s.hang {
		close(ch)
		delete(s.hang, k)
	}
}

// Calls lists the calls clients made, in order.
func (s *CodexServer) Calls() []CodexCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CodexCall(nil), s.calls...)
}

// CallsOf lists the calls of one method.
func (s *CodexServer) CallsOf(method string) []CodexCall {
	var out []CodexCall
	for _, c := range s.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// Replies lists the answers clients gave to server requests.
func (s *CodexServer) Replies() []CodexCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CodexCall(nil), s.replies...)
}

// Pending lists the server requests no one has answered.
func (s *CodexServer) Pending() []codexRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]codexRequest, 0, len(s.fold.pending))
	for _, r := range s.fold.pending {
		out = append(out, r)
	}
	return out
}

// Loaded lists the threads loaded now; Main is the terminal's.
func (s *CodexServer) Loaded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.fold.loaded...)
}

func (s *CodexServer) Main() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fold.main()
}

// Running is the thread's open turn and its items in progress.
func (s *CodexServer) Running(thread string) (string, []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fold.running(thread)
}

// Append writes messages to the journal: Codex doing something. The
// follower sends them on.
func (s *CodexServer) Append(msgs ...sessiongen.CodexMsg) {
	f, err := os.OpenFile(s.journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, m := range msgs {
		if b, err := json.Marshal(m); err == nil {
			_, _ = f.Write(append(b, '\n'))
		}
	}
}

// Flush waits until the journal written so far has been sent.
func (s *CodexServer) Flush() {
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		s.mu.Lock()
		done := s.off >= fileSize(s.journal)
		s.mu.Unlock()
		if done {
			return
		}
	}
}

// AnswerAtDesk is the request answered in the terminal: the server says
// it is resolved. It reports whether the request was still open.
func (s *CodexServer) AnswerAtDesk(id int) bool {
	s.mu.Lock()
	req, ok := s.fold.pending[requestKey(json.RawMessage(itoa(id)))]
	s.mu.Unlock()
	if ok {
		s.Append(sessiongen.CodexResolved(req.Thread, id))
		s.Flush()
	}
	return ok
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// follow sends the journal's new lines on as they are written.
func (s *CodexServer) follow(stop chan struct{}) {
	defer s.tailed.Done()
	t := time.NewTicker(5 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		s.mu.Lock()
		s.catchUpLocked()
		s.mu.Unlock()
	}
}

// catchUpLocked folds the journal's new lines and sends each to the
// clients that follow it: a new thread to every client, a thread's
// message to those that joined the thread, the account's to every one.
func (s *CodexServer) catchUpLocked() {
	lines, off := readLines(s.journal, s.off)
	s.off = off
	for _, l := range lines {
		var m codexWire
		if json.Unmarshal(l, &m) != nil {
			continue
		}
		s.fold.apply(m)
		th := m.threadOf()
		for c := range s.conns {
			if c.ready && (m.Method == "thread/started" || th == "" || c.subs[th]) {
				c.send(l)
			}
		}
	}
}

func (c *codexConn) send(b []byte) {
	select {
	case <-c.closed:
	case c.out <- b:
	default:
		c.close() // a client that stopped reading
	}
}

func (c *codexConn) close() {
	c.once.Do(func() {
		close(c.closed)
		_ = c.c.CloseNow()
	})
}

// Join is the terminal client joining a thread on this server (/resume at
// the desk): the thread is loaded.
func (s *CodexServer) Join(thread string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fold.load(thread)
}
