package enginetest

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

func freeTestPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func apiGet(t *testing.T, ctx context.Context, port int, path, user, pw string) *http.Response {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+path, nil)
	if user != "" {
		req.SetBasicAuth(user, pw)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// The server checks the password, streams the journal from its start on
// the first stream and from its end on a later one, and fails, drops and
// stops as told.
func TestAPIServer(t *testing.T) {
	home := t.TempDir()
	journal := sessiongen.OpenCodeJournal(home)
	if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, []byte(`{"type":"session.idle","properties":{"sessionID":"ses_1"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newAPIServer("opencode", home)
	port := freeTestPort(t)
	if err := s.Start(port, "pw", true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if res := apiGet(t, ctx, port, "/path", "", ""); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("no password: %d", res.StatusCode)
	} else {
		res.Body.Close()
	}
	read := func() (*http.Response, *bufio.Reader) {
		res := apiGet(t, ctx, port, "/event", "opencode", "pw")
		return res, bufio.NewReader(res.Body)
	}
	next := func(rd *bufio.Reader) string {
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return ""
			}
			if strings.HasPrefix(line, "data: ") && !strings.Contains(line, "server.connected") {
				return line
			}
		}
	}
	first, rd := read()
	if l := next(rd); !strings.Contains(l, "session.idle") {
		t.Fatalf("the first stream did not start at the journal's start: %q", l)
	}
	s.DropStreams()
	if _, err := io.ReadAll(first.Body); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Logf("drop: %v", err)
	}
	first.Body.Close()
	second, rd2 := read()
	defer second.Body.Close()
	s.Append(map[string]any{"type": "session.status", "properties": map[string]any{"sessionID": "ses_1", "status": map[string]any{"type": "busy"}}})
	if l := next(rd2); !strings.Contains(l, "busy") {
		t.Fatalf("a later stream did not start at the end: %q", l)
	}
	s.Fail("/session", http.StatusInternalServerError, 1)
	if res := apiGet(t, ctx, port, "/session", "opencode", "pw"); res.StatusCode != http.StatusInternalServerError {
		t.Errorf("fail: %d", res.StatusCode)
	} else {
		res.Body.Close()
	}
	if res := apiGet(t, ctx, port, "/session", "opencode", "pw"); res.StatusCode != http.StatusOK {
		t.Errorf("after one failure: %d", res.StatusCode)
	} else {
		res.Body.Close()
	}
	if res := apiGet(t, ctx, port, "/agent", "opencode", "pw"); res.StatusCode != http.StatusNotFound {
		t.Errorf("a route with no recorded answer: %d", res.StatusCode)
	} else {
		res.Body.Close()
	}
	s.Stop()
	c := http.Client{Timeout: time.Second}
	if res, err := c.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/path"); err == nil {
		res.Body.Close()
		t.Error("the stopped server still answers")
	}
}

// A held journal shows only its first bytes to the stream and the routes,
// whatever has been written after them, and the rest once released.
func TestAPIServerHold(t *testing.T) {
	s := &APIServer{journal: filepath.Join(t.TempDir(), "journal")}
	s.shown.Store(-1)
	write := func(text string) {
		f, err := os.OpenFile(s.journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(text); err != nil {
			t.Fatal(err)
		}
	}
	write("a\nb\n")
	s.Hold(int64(len("a\nb\n")))
	write("c\nd\n")
	lines, off := s.read(0)
	if len(lines) != 2 || off != 4 || s.size() != 4 {
		t.Fatalf("held: %q off %d size %d", lines, off, s.size())
	}
	s.Hold(int64(len("a\nb\nc\n")))
	if lines, off = s.read(off); len(lines) != 1 || string(lines[0]) != "c" {
		t.Fatalf("one more event: %q", lines)
	}
	s.Hold(-1)
	if lines, _ = s.read(off); len(lines) != 1 || string(lines[0]) != "d" {
		t.Fatalf("released: %q", lines)
	}
}

// A read of the journal that is under way when a turn is held and written
// shows none of the turn: hold (apifed) sets the limit, then writes, and a
// stream must never see the held part, even for one read.
func TestAPIServerHoldDuringRead(t *testing.T) {
	s := &APIServer{journal: filepath.Join(t.TempDir(), "journal")}
	s.shown.Store(-1)
	if err := os.WriteFile(s.journal, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reached, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.midRead = func() {
		once.Do(func() {
			close(reached)
			<-resume
		})
	}
	got := make(chan [][]byte)
	go func() {
		lines, _ := s.read(0)
		got <- lines
	}()
	<-reached
	s.Hold(int64(len("a\n")))
	f, err := os.OpenFile(s.journal, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("held\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	close(resume)
	if lines := <-got; len(lines) != 1 || string(lines[0]) != "a" {
		t.Fatalf("a read under way showed %q", lines)
	}
}

// A stream counted in Streams has its start fixed: what is written after a
// test saw the stream come back is sent on it (apifed waits for Streams,
// then writes the next turn).
func TestAPIServerStreamStartsWhenCounted(t *testing.T) {
	home := t.TempDir()
	journal := sessiongen.OpenCodeJournal(home)
	if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, []byte(`{"type":"session.idle","properties":{"sessionID":"ses_1"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newAPIServer("opencode", home)
	counted, resume := make(chan struct{}), make(chan struct{})
	s.opened = func() {
		close(counted)
		<-resume
	}
	port := freeTestPort(t)
	if err := s.Start(port, "pw", false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res := make(chan *http.Response)
	go func() { res <- apiGet(t, ctx, port, "/event", "opencode", "pw") }()
	<-counted
	if s.Streams() != 1 {
		t.Fatalf("streams: %d", s.Streams())
	}
	status := func(v string) map[string]any {
		return map[string]any{"type": "session.status", "properties": map[string]any{"sessionID": "ses_1", "status": map[string]any{"type": v}}}
	}
	s.Append(status("busy"))
	close(resume)
	r := <-res
	defer r.Body.Close()
	s.Append(status("retry"))
	rd := bufio.NewReader(r.Body)
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") && !strings.Contains(line, "server.connected") {
			if !strings.Contains(line, "busy") {
				t.Fatalf("the event written once the stream was counted was not sent; first: %q", line)
			}
			return
		}
	}
}
