package enginetest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// APIServer stands in for the local HTTP server of an agent whose chat
// the helper reads over HTTP (OpenCode 1.x and Kilo Code). The helper
// reaches it as it reaches the real one: the rig asks the engine for the
// launch arguments (EndpointArgs), and the server listens on the port and
// checks the password they name.
//
// Its stream is a journal file (sessiongen.OpenCodeJournal), one event per
// line: /event sends each line as one server-sent event as it is written.
// The first stream of a run starts at the run's first event (the helper
// was connected from the launch); a stream opened later starts at the end
// of the journal, as the real stream has no history. The routes the
// adapter reads are answered from what the journal holds; the calls the
// helper makes are recorded and, where the agent answers on its stream,
// answered there with the event the real agent sends.
//
// It answers only routes with real recorded answers or recorded events
// to build them from; any other route is 404, as for a server that lacks
// it. It can be told to fail calls, answer slowly, drop its streams,
// stop, and start again on another port.
type APIServer struct {
	journal   string
	providers []byte
	user      string

	mu       sync.Mutex
	password string
	srv      *http.Server
	port     int
	from     int64 // where the next stream starts, -1 for the end
	streams  map[chan struct{}]bool
	calls    []APICall
	fail     []failRule
	delay    time.Duration
	lost     map[string]bool
	// OnPrompt is the agent taking a prompt; OnAbort an interrupt. They
	// run outside the server's lock.
	OnPrompt func(text string)
	OnAbort  func()
}

// APICall is one request the helper made.
type APICall struct {
	Method, Path string
	Body         string
	Status       int
}

type failRule struct {
	prefix string
	status int
	left   int
}

// apiUsers are the Basic auth user names of the agents (opencode.yaml:10,
// kilo.yaml:14).
var apiUsers = map[string]string{"opencode": "opencode", "kilo": "kilo"}

// IsAPIAgent reports an agent the rig serves through an APIServer.
func IsAPIAgent(agent string) bool { _, ok := apiUsers[agent]; return ok }

// newAPIServer makes a server for the agent whose journal is under home.
// It serves nothing until Start.
func newAPIServer(agent, home string) *APIServer {
	dir := "opencode"
	if agent == "kilo" {
		dir = "kilo"
	}
	prov, _ := os.ReadFile(filepath.Join(sourceDir(), "harness", "adapters", dir, "testdata", "providers.json"))
	return &APIServer{journal: sessiongen.OpenCodeJournal(home), providers: prov, user: apiUsers[agent],
		streams: map[chan struct{}]bool{}, lost: map[string]bool{}}
}

// Start listens on port with the password the agent was given. fromStart
// makes the first stream begin at the journal's start (the helper was
// there from the agent's launch); otherwise it begins at the end.
func (s *APIServer) Start(port int, password string, fromStart bool) error {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.password, s.port, s.from = password, port, -1
	if fromStart {
		s.from = 0
	}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: 5 * time.Second}
	srv := s.srv
	s.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// Stop ends the server and every stream, as the agent's process ending.
func (s *APIServer) Stop() {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	s.port = 0
	s.mu.Unlock()
	if srv == nil {
		return
	}
	s.DropStreams()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	_ = srv.Close()
}

// Port is where it listens, 0 when stopped.
func (s *APIServer) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// DropStreams ends every open stream, as a network drop: the helper's feed
// reconnects, and what is written meanwhile is not sent to it.
func (s *APIServer) DropStreams() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.streams {
		close(c)
		delete(s.streams, c)
	}
}

// Streams is how many streams are open.
func (s *APIServer) Streams() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

// Fail makes the next n requests whose path starts with prefix answer
// status; n < 0 fails them all until Heal.
func (s *APIServer) Fail(prefix string, status, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = append(s.fail, failRule{prefix: prefix, status: status, left: n})
}

// Heal ends every Fail and Slow.
func (s *APIServer) Heal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail, s.delay = nil, 0
}

// Slow makes every request but the stream wait d before it is answered.
func (s *APIServer) Slow(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = d
}

// Lose makes the agent forget a session: its routes answer 404.
func (s *APIServer) Lose(session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lost[session] = true
}

// Calls lists the requests made so far, the stream's included.
func (s *APIServer) Calls() []APICall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]APICall(nil), s.calls...)
}

// Posts lists the calls that were not GET, in order.
func (s *APIServer) Posts() []APICall {
	var out []APICall
	for _, c := range s.Calls() {
		if c.Method != http.MethodGet {
			out = append(out, c)
		}
	}
	return out
}

// Append writes events to the journal, as the agent doing something.
func (s *APIServer) Append(events ...any) {
	f, err := os.OpenFile(s.journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err == nil {
			_, _ = f.Write(append(b, '\n'))
		}
	}
}

func (s *APIServer) authorized(r *http.Request) bool {
	s.mu.Lock()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(s.user+":"+s.password))
	s.mu.Unlock()
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) == 1
}

func (s *APIServer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	call := APICall{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)}
	status := s.answer(w, r, body, &call)
	call.Status = status
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()
}

// answer writes the response and returns its status.
func (s *APIServer) answer(w http.ResponseWriter, r *http.Request, body []byte, call *APICall) int {
	if !s.authorized(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return http.StatusUnauthorized
	}
	if r.URL.Path == "/event" {
		return s.stream(w, r)
	}
	s.mu.Lock()
	delay := s.delay
	status := 0
	for i := range s.fail {
		f := &s.fail[i]
		if f.left != 0 && strings.HasPrefix(r.URL.Path, f.prefix) {
			status = f.status
			if f.left > 0 {
				f.left--
			}
			break
		}
	}
	s.mu.Unlock()
	if delay > 0 {
		select {
		case <-r.Context().Done():
			return 499
		case <-time.After(delay):
		}
	}
	if status != 0 {
		w.WriteHeader(status)
		return status
	}
	st, out := s.route(r.Method, r.URL.Path, body)
	if out == nil {
		w.WriteHeader(st)
		return st
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(st)
	_, _ = w.Write(out)
	return st
}

// stream sends the journal's events from where this stream starts until
// the client goes, the stream is dropped or the server stops.
func (s *APIServer) stream(w http.ResponseWriter, r *http.Request) int {
	fl, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	done := make(chan struct{})
	s.mu.Lock()
	from := s.from
	s.from = -1
	s.streams[done] = true
	s.mu.Unlock()
	if from < 0 {
		from = fileSize(s.journal)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	// The real server's first event (events.sse:1).
	_, _ = io.WriteString(w, "data: {\"type\":\"server.connected\",\"properties\":{}}\n\n")
	fl.Flush()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var lines [][]byte
		lines, from = readLines(s.journal, from)
		for _, l := range lines {
			if _, err := w.Write(append(append([]byte("data: "), l...), '\n', '\n')); err != nil {
				return http.StatusOK
			}
		}
		if len(lines) > 0 {
			fl.Flush()
		}
		select {
		case <-r.Context().Done():
			s.forget(done)
			return http.StatusOK
		case <-done:
			return http.StatusOK
		case <-tick.C:
		}
	}
}

func (s *APIServer) forget(done chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streams[done] {
		close(done)
		delete(s.streams, done)
	}
}

func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

// readLines returns the whole lines written after off, and where the
// next read starts.
func readLines(path string, off int64) ([][]byte, int64) {
	f, err := os.Open(path)
	if err != nil {
		return nil, off
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() < off {
		off = 0 // the file was cut and written again
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, off
	}
	var out [][]byte
	rd := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return out, off
		}
		off += int64(len(line))
		if l := bytes.TrimSpace(line); len(l) > 0 {
			out = append(out, l)
		}
	}
}
