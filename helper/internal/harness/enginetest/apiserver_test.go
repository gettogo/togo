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
