package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
)

func init() {
	harness.RegisterAdapterFile("fakehistp", []byte(`
adapter: fakehistp
name: HistP
revision: 1
match: { agents: [fakehistp] }
capabilities: [chat, chat.history]
feeds:
  events:
    hook: { events: [Event] }
    history:
      path: '/log/{{.conversation}}?limit=2&order=desc'
      items: '.data | reverse'
      page: { cursor: '.cursor.next', param: cursor, omit: [order], max_pages: 2 }
    mapper: fake.events
    stability: documented
`))
}

// A chat whose past could not all be read shows what was read, oldest
// first, and then one fixed line saying earlier messages are in the
// terminal. The line names no error and no cursor.
func TestAChatWithOlderUnreadMessagesSaysSoAfterThem(t *testing.T) {
	const id = "s_fakehistpn"
	home, togo := t.TempDir(), t.TempDir()
	var records []string
	for i := 1; i <= 5; i++ {
		records = append(records, histEvent(fmt.Sprintf("e%d", i), "A", fmt.Sprintf("a%d", i), fmt.Sprintf("said %d", i)))
	}
	route := &pagedRoute{records: records, size: 2, cursor: func(o int) string { return fmt.Sprintf("secret-cursor-%d", o) }}
	srv := httptest.NewServer(route)
	t.Cleanup(srv.Close)
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakehistp", Managed: true, Cwd: home, Command: []string{"fakehistp"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home, TogoHome: togo,
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	e.eps.mu.Lock()
	e.eps.byID = map[string]endpoint{id: {kind: spec.EndpointHTTP, port: port, adapter: "fakehistp"}}
	e.eps.mu.Unlock()
	ctx := context.Background()
	if _, err := e.Describe(ctx, id); err != nil {
		t.Fatal(err)
	}
	chat := func() []string {
		res, err := e.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range res.Items {
			switch {
			case it.User != nil:
				out = append(out, it.User.Text)
			case it.Notice != nil:
				out = append(out, "NOTICE:"+it.Notice.Text)
			}
		}
		return out
	}
	deliver(id, json.RawMessage(histEvent("e5", "A", "a5", "said 5")))
	waitFor(t, func() bool { return len(chat()) == 5 })
	got := chat()
	want := []string{"said 2", "said 3", "said 4", "said 5", "NOTICE:" + olderInTerminal}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chat = %q, want %q", got, want)
	}
	if strings.Contains(strings.Join(got, "|"), "secret-cursor") {
		t.Error("the notice repeats the cursor")
	}
}

// A route's answer that cannot be read as a page is not taken for the start
// of the conversation: a cursor that is a number or an object, or data that
// is not a list, leaves the read partial or failed, never complete.
func TestAnAnswerThatIsNotAPageIsNotTheStart(t *testing.T) {
	for name, body := range map[string]string{
		"a numeric cursor": `{"data":[{"n":2}],"cursor":{"next":17}}`,
		"an object cursor": `{"data":[{"n":2}],"cursor":{"next":{"unexpected":"shape"}}}`,
		"data not a list":  `{"data":{"n":2},"cursor":{}}`,
	} {
		e, s := pagedEngine(t, &pagedRoute{})
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, body)
		}))
		u, _ := url.Parse(srv.URL)
		port, _ := strconv.Atoi(u.Port())
		e.eps.mu.Lock()
		e.eps.byID[s.id] = endpoint{kind: spec.EndpointHTTP, port: port, adapter: "fakehist"}
		e.eps.mu.Unlock()
		s.env.Endpoint = srv.URL
		h := pagedHistory(0)
		h.Items = ".data"
		events, partial, err := e.readHistory(context.Background(), s, h, "A")
		srv.Close()
		if !called {
			t.Fatalf("%s: the route was not read", name)
		}
		if err == nil && !partial {
			t.Errorf("%s: taken for a complete past (%d records)", name, len(events))
		}
	}
	// An absent, null or empty cursor does say the start was reached.
	for name, body := range map[string]string{
		"no cursor":    `{"data":[{"n":1}]}`,
		"null cursor":  `{"data":[{"n":1}],"cursor":{"next":null}}`,
		"empty cursor": `{"data":[{"n":1}],"cursor":{"next":""}}`,
	} {
		e, s := pagedEngine(t, &pagedRoute{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, body)
		}))
		u, _ := url.Parse(srv.URL)
		port, _ := strconv.Atoi(u.Port())
		e.eps.mu.Lock()
		e.eps.byID[s.id] = endpoint{kind: spec.EndpointHTTP, port: port, adapter: "fakehist"}
		e.eps.mu.Unlock()
		s.env.Endpoint = srv.URL
		h := pagedHistory(0)
		h.Items = ".data"
		events, partial, err := e.readHistory(context.Background(), s, h, "A")
		srv.Close()
		if err != nil || partial || len(events) != 1 {
			t.Errorf("%s: %d records partial %v err %v, want the whole past", name, len(events), partial, err)
		}
	}
}

// The line saying older messages are not shown is an ordinary item after
// the replay: a replay that fills what a session keeps cannot push it out,
// and a chat that reconnects asking for what came after a number it had
// receives it. A replay that alone overflows what is kept gets the line
// too, though the route was read to its start.
func TestTheOlderMessagesLineSurvivesAFullChatAndAReconnect(t *testing.T) {
	for name, tc := range map[string]struct {
		pages int
		keep  int
	}{
		"a partial read into a full chat": {2, 3},
		"a whole read that overflows":     {0, 3},
	} {
		const id = "s_fakehistpf"
		home, togo := t.TempDir(), t.TempDir()
		var records []string
		for i := 1; i <= 5; i++ {
			records = append(records, histEvent(fmt.Sprintf("e%d", i), "A", fmt.Sprintf("a%d", i), fmt.Sprintf("said %d", i)))
		}
		route := &pagedRoute{records: records, size: 2, cursor: func(o int) string { return fmt.Sprintf("c%d", o) }}
		srv := httptest.NewServer(route)
		agent := "fakehistp"
		if tc.pages == 0 {
			agent = "fakehistw"
		}
		e := New(Config{
			Sessions: fakeSessions{id: session.Session{ID: id, Agent: agent, Managed: true, Cwd: home, Command: []string{agent}}},
			Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home, TogoHome: togo,
			LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
			Log:       &Log{Max: tc.keep, NewEpoch: func() string { return "e1" }}, Scale: 0.1,
		})
		u, _ := url.Parse(srv.URL)
		port, _ := strconv.Atoi(u.Port())
		e.eps.mu.Lock()
		e.eps.byID = map[string]endpoint{id: {kind: spec.EndpointHTTP, port: port, adapter: agent}}
		e.eps.mu.Unlock()
		ctx := context.Background()
		if _, err := e.Describe(ctx, id); err != nil {
			t.Fatal(err)
		}
		chat := func() []string {
			res, err := e.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, 50)
			if err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, it := range res.Items {
				switch {
				case it.User != nil:
					out = append(out, it.User.Text)
				case it.Notice != nil:
					out = append(out, "NOTICE:"+it.Notice.Text)
				}
			}
			return out
		}
		deliver(id, json.RawMessage(histEvent("e5", "A", "a5", "said 5")))
		waitFor(t, func() bool { c := chat(); return len(c) == tc.keep && strings.HasPrefix(c[len(c)-1], "NOTICE:") })
		got := chat()
		want := []string{"said 4", "said 5", "NOTICE:" + olderInTerminal}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: chat = %q, want %q", name, got, want)
		}
		// Every number is above the dropped ones and ascending, and a
		// client that held any earlier number is sent the line.
		floor := e.log.RetentionFloor(id)
		items, _, _ := e.log.Since(id, 0, 50)
		last := floor
		for _, it := range items {
			if it.Seq <= last {
				t.Errorf("%s: numbers not ascending above the floor: %d after %d", name, it.Seq, last)
			}
			last = it.Seq
		}
		for after := int64(0); after < items[len(items)-1].Seq; after++ {
			since, _, _ := e.log.Since(id, after, 50)
			if len(since) == 0 || since[len(since)-1].Notice == nil {
				t.Errorf("%s: a chat that had up to %d is not sent the line", name, after)
			}
		}
		e.Forget(id)
		srv.Close()
	}
}

func init() {
	harness.RegisterAdapterFile("fakehistw", []byte(`
adapter: fakehistw
name: HistW
revision: 1
match: { agents: [fakehistw] }
capabilities: [chat, chat.history]
feeds:
  events:
    hook: { events: [Event] }
    history:
      path: '/log/{{.conversation}}?limit=2&order=desc'
      items: '.data | reverse'
      page: { cursor: '.cursor.next', param: cursor, omit: [order] }
    mapper: fake.events
    stability: documented
`))
}

// A past that cannot be read at all leaves the chat with what happens from
// now on, and says so: recent messages alone are not passed off as the
// whole conversation. The read is tried again on later records a bounded
// number of times; when one succeeds the past appears and the same line
// says it has been loaded.
func TestAPastThatCannotBeReadIsSaidAndTriedAgain(t *testing.T) {
	const id = "s_fakehistpx"
	home, togo := t.TempDir(), t.TempDir()
	var records []string
	for i := 1; i <= 3; i++ {
		records = append(records, histEvent(fmt.Sprintf("e%d", i), "A", fmt.Sprintf("a%d", i), fmt.Sprintf("said %d", i)))
	}
	route := &pagedRoute{records: records, size: 5, cursor: func(o int) string { return fmt.Sprintf("c%d", o) }, fail: map[int]bool{0: true}}
	srv := httptest.NewServer(route)
	t.Cleanup(srv.Close)
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakehistw", Managed: true, Cwd: home, Command: []string{"fakehistw"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home, TogoHome: togo,
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	e.eps.mu.Lock()
	e.eps.byID = map[string]endpoint{id: {kind: spec.EndpointHTTP, port: port, adapter: "fakehistw"}}
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
			switch {
			case it.User != nil:
				out = append(out, it.User.Text)
			case it.Notice != nil:
				out = append(out, "NOTICE:"+it.Notice.Text)
			}
		}
		return strings.Join(out, "|")
	}
	reads := func() int {
		route.mu.Lock()
		defer route.mu.Unlock()
		return len(route.requests)
	}
	deliver(id, json.RawMessage(histEvent("e3", "A", "a3", "said 3")))
	waitFor(t, func() bool {
		return chat() == "NOTICE:"+olderInTerminal+"|said 3" || chat() == "said 3|NOTICE:"+olderInTerminal
	})
	// Each later record asks again, up to the bound, and no further.
	for i := 4; i <= 8; i++ {
		deliver(id, json.RawMessage(histEvent(fmt.Sprintf("e%d", i), "A", fmt.Sprintf("a%d", i), fmt.Sprintf("said %d", i))))
		waitFor(t, func() bool { return strings.Contains(chat(), fmt.Sprintf("said %d", i)) })
	}
	if n := reads(); n != historyRetries {
		t.Errorf("the past was asked for %d times, want %d", n, historyRetries)
	}
	if n := strings.Count(chat(), "NOTICE:"); n != 1 {
		t.Errorf("the line shows %d times: %q", n, chat())
	}
}

// When a read that failed is followed by one that succeeds, the past
// appears and the line is replaced by one saying so.
func TestAPastReadOnALaterTryReplacesTheLine(t *testing.T) {
	const id = "s_fakehistpy"
	home, togo := t.TempDir(), t.TempDir()
	var records []string
	for i := 1; i <= 3; i++ {
		records = append(records, histEvent(fmt.Sprintf("e%d", i), "A", fmt.Sprintf("a%d", i), fmt.Sprintf("said %d", i)))
	}
	route := &pagedRoute{records: records, size: 5, cursor: func(o int) string { return fmt.Sprintf("c%d", o) }, fail: map[int]bool{0: true}}
	srv := httptest.NewServer(route)
	t.Cleanup(srv.Close)
	e := New(Config{
		Sessions: fakeSessions{id: session.Session{ID: id, Agent: "fakehistw", Managed: true, Cwd: home, Command: []string{"fakehistw"}}},
		Typist:   &seqPane{}, Marker: &fakeMarker{}, Home: home, TogoHome: togo,
		LoginPath: func(context.Context) string { return "/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	e.eps.mu.Lock()
	e.eps.byID = map[string]endpoint{id: {kind: spec.EndpointHTTP, port: port, adapter: "fakehistw"}}
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
			switch {
			case it.User != nil:
				out = append(out, it.User.Text)
			case it.Notice != nil:
				out = append(out, "NOTICE:"+it.Notice.Text)
			}
		}
		return strings.Join(out, "|")
	}
	deliver(id, json.RawMessage(histEvent("e3", "A", "a3", "said 3")))
	waitFor(t, func() bool { return strings.Contains(chat(), "NOTICE:"+olderInTerminal) })
	route.mu.Lock()
	route.fail = nil
	route.mu.Unlock()
	deliver(id, json.RawMessage(histEvent("e4", "A", "a4", "said 4")))
	waitFor(t, func() bool {
		return strings.Contains(chat(), "said 1") && strings.Contains(chat(), "NOTICE:"+olderLoaded)
	})
	got := chat()
	if strings.Contains(got, olderInTerminal) || strings.Count(got, "NOTICE:") != 1 {
		t.Errorf("after the past was read: %q", got)
	}
	for _, said := range []string{"said 1", "said 2", "said 3", "said 4"} {
		if strings.Count(got, said) != 1 {
			t.Errorf("%q shows %d times in %q", said, strings.Count(got, said), got)
		}
	}
}
