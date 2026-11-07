package httpsrc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func envFor(srv *httptest.Server) harness.Env {
	return harness.Env{SessionID: "s_test", Endpoint: srv.URL}
}

func fetch(t *testing.T, env harness.Env, b spec.HTTP) (json.RawMessage, error) {
	t.Helper()
	raw, _ := json.Marshal(b)
	return HTTP{}.Fetch(context.Background(), harness.SourceSpec{Name: "x", Kind: spec.KindHTTP, Raw: raw}, env)
}

func TestBaseLoopbackOnly(t *testing.T) {
	good := []string{"http://127.0.0.1:4096", "127.0.0.1:4096", "http://localhost:80", "http://[::1]:9"}
	for _, ep := range good {
		if _, err := Base(harness.Env{Endpoint: ep}); err != nil {
			t.Errorf("%s refused: %v", ep, err)
		}
	}
	bad := []string{"http://10.0.0.1:4096", "http://example.com:80", "https://127.0.0.1:1", "http://127.0.0.1",
		"http://u:p@127.0.0.1:1", "http://127.0.0.1:1/api", "http://0.0.0.0:1", "http://127.0.0.1:99999"}
	for _, ep := range bad {
		if _, err := Base(harness.Env{Endpoint: ep}); err == nil {
			t.Errorf("%s accepted", ep)
		}
	}
	u, err := Base(harness.Env{Vars: map[string]string{VarPort: "4100"}})
	if err != nil || u.String() != "http://127.0.0.1:4100" {
		t.Fatalf("port var: %v %v", u, err)
	}
}

func TestJoinRejectsEscapes(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:1")
	for _, p := range []string{"session", "//evil.com/x", "http://evil.com/", "/a/../b", "/a\\b", "/a#f", "/a b", "/a\nb", "/./x"} {
		if _, err := Join(base, p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
	u, err := Join(base, "/session/ses_1/todo?directory=%2Ftmp")
	if err != nil || u.String() != "http://127.0.0.1:1/session/ses_1/todo?directory=%2Ftmp" {
		t.Fatalf("join: %v %v", u, err)
	}
}

func TestFetchScrubsProviders(t *testing.T) {
	body, err := os.ReadFile("testdata/providers.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/providers" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	out, err := fetch(t, envFor(srv), spec.HTTP{Path: "/config/providers"})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(out); strings.Contains(s, "sk-") || strings.Contains(s, "secret-value") || strings.Contains(s, "local-secret") {
		t.Fatalf("key left the runner: %s", s)
	}
}

func TestFetchMethodsBodyAndStatus(t *testing.T) {
	var gotBody, gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotType = string(b), r.Header.Get("Content-Type")
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(204)
		case "/text":
			w.Write([]byte("plain sk-abcdefghijklmnopqrst"))
		case "/fail":
			http.Error(w, "secret detail", 500)
		case "/redirect":
			http.Redirect(w, r, "http://example.com/", http.StatusFound)
		}
	}))
	defer srv.Close()
	out, err := fetch(t, envFor(srv), spec.HTTP{Method: "post", Path: "/ok", Body: `{"reply":"once"}`})
	if err != nil || string(out) != "null" || gotBody != `{"reply":"once"}` || gotType != "application/json" {
		t.Fatalf("post: %s %v %q %q", out, err, gotBody, gotType)
	}
	out, err = fetch(t, envFor(srv), spec.HTTP{Path: "/text"})
	if err != nil || !strings.Contains(string(out), Marker) || strings.Contains(string(out), "sk-abc") {
		t.Fatalf("text: %s %v", out, err)
	}
	_, err = fetch(t, envFor(srv), spec.HTTP{Path: "/fail"})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 500 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("status error: %v", err)
	}
	if _, err = fetch(t, envFor(srv), spec.HTTP{Path: "/redirect"}); err == nil {
		t.Fatal("redirect followed or accepted")
	}
	if _, err = fetch(t, envFor(srv), spec.HTTP{Method: "TRACE", Path: "/ok"}); err == nil {
		t.Fatal("TRACE allowed")
	}
	if _, err = fetch(t, envFor(srv), spec.HTTP{Method: "POST", Path: "/ok", Body: "{not json"}); err == nil {
		t.Fatal("invalid body sent")
	}
}

func TestFetchBodyCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`"`))
		w.Write([]byte(strings.Repeat("a", MaxBody+10)))
		w.Write([]byte(`"`))
	}))
	defer srv.Close()
	if _, err := fetch(t, envFor(srv), spec.HTTP{Path: "/big"}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("cap: %v", err)
	}
}

func TestFetchRefusesRemoteEndpoint(t *testing.T) {
	_, err := fetch(t, harness.Env{Endpoint: "http://192.0.2.1:80"}, spec.HTTP{Path: "/path"})
	if !errors.Is(err, ErrNotLoopback) {
		t.Fatalf("remote endpoint: %v", err)
	}
	// The dialer itself refuses a non-loopback address, whatever built it.
	if err := loopbackOnly("tcp", "192.0.2.1:80", nil); !errors.Is(err, ErrNotLoopback) {
		t.Fatalf("dial control: %v", err)
	}
	if err := loopbackOnly("tcp", "127.0.0.1:80", nil); err != nil {
		t.Fatalf("dial control loopback: %v", err)
	}
}
