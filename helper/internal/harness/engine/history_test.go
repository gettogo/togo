package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakehist", []byte(`
adapter: fakehist
name: Hist
revision: 1
match: { agents: [fakehist] }
capabilities: [chat, chat.history]
feeds:
  events:
    hook: { events: [Event] }
    history: { path: '/log/{{.conversation}}' }
    mapper: fake.events
    stability: documented
`))
}

// histServer serves a conversation's past as an event stream that ends
// with log.synced, as OpenCode's session log does.
func histServer(t *testing.T, logs map[string][]string, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		conv := strings.TrimPrefix(r.URL.Path, "/log/")
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range logs[conv] {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		fmt.Fprint(w, "data: {\"type\":\"log.synced\"}\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func histEvent(id, conv, item, text string) string {
	b, _ := json.Marshal(map[string]any{"id": id, "state": map[string]string{"conversation": conv},
		"items": []harness.Item{{ID: item, TurnID: item, Kind: harness.KindUser, User: &harness.UserBody{Text: text}}}})
	return string(b)
}

// Seen live on OpenCode 2.3: after a helper restart the agent and its
// conversation were unchanged and a freshly opened chat was empty; only
// what was said afterwards appeared. A feed with a history loads its
// conversation's past: when the chat learns which conversation it shows,
// when it moves to another one, and after a restart.
func TestConversationPastIsLoaded(t *testing.T) {
	const id = "s_fakehistaa"
	home, togo := t.TempDir(), t.TempDir()
	var hits atomic.Int64
	srv := histServer(t, map[string][]string{
		"A": {histEvent("e1", "A", "a1", "earlier one"), histEvent("e2", "A", "a2", "earlier two"), histEvent("e3", "A", "a3", "live three")},
		"B": {histEvent("f1", "B", "b1", "other earlier")},
	}, &hits)
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakehist", Managed: true, Cwd: home, Command: []string{"fakehist"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home, TogoHome: togo,
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	// The session's harness endpoint is the test server.
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	e.eps.mu.Lock()
	e.eps.byID = map[string]endpoint{id: {kind: spec.EndpointHTTP, port: port, adapter: "fakehist"}}
	e.eps.mu.Unlock()
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	chat := func() string {
		res, err := e.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range res.Items {
			if it.User != nil {
				out = append(out, it.User.Text)
			}
		}
		return strings.Join(out, " | ")
	}
	// A live event names conversation A: its past loads, the live event is
	// not shown twice though the log holds it too.
	deliver(id, json.RawMessage(histEvent("e3", "A", "a3", "live three")))
	waitFor(t, func() bool {
		return chat() == "live three | earlier one | earlier two" || chat() == "earlier one | earlier two | live three"
	})
	if n := strings.Count(chat(), "live three"); n != 1 {
		t.Errorf("the live event shows %d times: %q", n, chat())
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("history read %d times, want once per conversation", n)
	}
	deliver(id, json.RawMessage(histEvent("e4", "A", "a4", "after")))
	waitFor(t, func() bool { return strings.Contains(chat(), "after") })
	if n := hits.Load(); n != 1 {
		t.Errorf("history read again for the same conversation (%d)", n)
	}
	// The conversation it shows is remembered for a helper restart.
	waitFor(t, func() bool { return e.loadConversation(id) == "A" })
	if fi, err := os.Stat(filepath.Join(togo, "run", "harness", id+".conversation")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("conversation file: %v %v", fi, err)
	}
	// The agent moves to conversation B: the chat starts over and B's past loads.
	deliver(id, json.RawMessage(histEvent("f2", "B", "b2", "other live")))
	waitFor(t, func() bool {
		c := chat()
		return strings.Contains(c, "other earlier") && strings.Contains(c, "other live")
	})
	if c := chat(); strings.Contains(c, "earlier one") {
		t.Errorf("the old conversation is still in the chat: %q", c)
	}
	waitFor(t, func() bool { return e.loadConversation(id) == "B" })
	e.Forget(id)
	if got := e.loadConversation(id); got != "" {
		t.Errorf("a removed session still remembers %q", got)
	}
}

// After a helper restart nothing has happened yet on the stream: the
// conversation the session showed before is loaded from what was saved.
func TestConversationPastIsLoadedAfterAHelperRestart(t *testing.T) {
	const id = "s_fakehistbb"
	home, togo := t.TempDir(), t.TempDir()
	var hits atomic.Int64
	srv := histServer(t, map[string][]string{"A": {histEvent("e1", "A", "a1", "said before the restart")}}, &hits)
	dir := filepath.Join(togo, "run", "harness")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".conversation"), []byte("A"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakehist", Managed: true, Cwd: home, Command: []string{"fakehist"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home, TogoHome: togo,
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	e.eps.mu.Lock()
	e.eps.byID = map[string]endpoint{id: {kind: spec.EndpointHTTP, port: port, adapter: "fakehist"}}
	e.eps.mu.Unlock()
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		res, err := e.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, 50)
		return err == nil && len(res.Items) == 1 && res.Items[0].User != nil && res.Items[0].User.Text == "said before the restart"
	})
}
