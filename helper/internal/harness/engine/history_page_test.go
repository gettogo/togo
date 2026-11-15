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
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
)

// pagedRoute serves records newest first in pages, as OpenCode's messages
// route does: the first request names an order, later ones carry only the
// cursor of the answer before, and a cursor sent together with an order is
// refused.
type pagedRoute struct {
	mu       sync.Mutex
	records  []string // oldest first
	size     int
	requests []string
	cursor   func(offset int) string // the cursor for the page starting at offset
	fail     map[int]bool            // offsets whose page answers 500
	overlap  int                     // records each older page repeats from the newer one
	empty    map[int]bool            // offsets whose page is empty but still names a cursor
}

func (p *pagedRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	q := r.URL.Query()
	p.requests = append(p.requests, r.URL.RawQuery)
	offset := 0
	if c := q.Get("cursor"); c != "" {
		if q.Has("order") {
			http.Error(w, "cursor with order", http.StatusBadRequest)
			return
		}
		found := false
		for o := 0; o <= len(p.records)+p.size; o++ {
			if p.cursor(o) == c {
				offset, found = o, true
				break
			}
		}
		if !found {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
	} else if q.Get("order") != "desc" {
		http.Error(w, "no order", http.StatusBadRequest)
		return
	}
	if p.fail[offset] {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	// Newest first: offset counts from the newest record.
	var page []string
	start := max(offset-p.overlap, 0)
	if !p.empty[offset] {
		for i := start; i < offset+p.size && i < len(p.records); i++ {
			page = append(page, p.records[len(p.records)-1-i])
		}
	}
	next := "null"
	if offset+p.size < len(p.records) {
		b, _ := json.Marshal(p.cursor(offset + p.size))
		next = string(b)
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"data":[%s],"cursor":{"next":%s}}`, strings.Join(page, ","), next)
}

func pagedEngine(t *testing.T, route *pagedRoute) (*Engine, *sess) {
	t.Helper()
	const id = "s_fakehistpg"
	home, togo := t.TempDir(), t.TempDir()
	srv := httptest.NewServer(route)
	t.Cleanup(srv.Close)
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
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return e, s
}

func pagedHistory(pages int) *spec.History {
	return &spec.History{Path: "/log/{{.conversation}}?limit=3&order=desc", Items: ".data | reverse",
		Page: &spec.HistoryPage{Cursor: ".cursor.next", Param: "cursor", Omit: []string{"order"}, MaxPages: pages}}
}

func numbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf(`{"n":%d}`, i+1)
	}
	return out
}

func order(events []json.RawMessage) string {
	var ns []string
	for _, ev := range events {
		var rec struct {
			N int `json:"n"`
		}
		_ = json.Unmarshal(ev, &rec)
		ns = append(ns, strconv.Itoa(rec.N))
	}
	return strings.Join(ns, ",")
}

// The newest page is read first and older pages are put in front of it,
// until the route names no older page: the past comes out whole and oldest
// first. The cursor goes back exactly as it came, as a query value, and
// without the order the first request named.
func TestHistoryIsReadPageByPageBackToItsStart(t *testing.T) {
	route := &pagedRoute{records: numbered(8), size: 3, cursor: func(o int) string { return fmt.Sprintf("c+/=&x %d", o) }}
	e, s := pagedEngine(t, route)
	events, partial, err := e.readHistory(context.Background(), s, pagedHistory(0), "A")
	if err != nil || partial || order(events) != "1,2,3,4,5,6,7,8" {
		t.Fatalf("history = %s partial %v err %v", order(events), partial, err)
	}
	if len(route.requests) != 3 || route.requests[0] != "limit=3&order=desc" {
		t.Fatalf("requests = %q", route.requests)
	}
	for _, raw := range route.requests[1:] {
		q, _ := url.ParseQuery(raw)
		if q.Has("order") || q.Get("limit") != "3" || !strings.HasPrefix(q.Get("cursor"), "c+/=&x ") {
			t.Errorf("a cursor request was %q", raw)
		}
	}
	// A route with no paging rule is read once, as before.
	route.requests = nil
	events, partial, err = e.readHistory(context.Background(), s, &spec.History{Path: "/log/{{.conversation}}?limit=3&order=desc", Items: ".data | reverse"}, "A")
	if err != nil || partial || order(events) != "6,7,8" || len(route.requests) != 1 {
		t.Errorf("unpaged history = %s partial %v err %v requests %d", order(events), partial, err, len(route.requests))
	}
}

// When reading stops before the start, what was read is kept, the newest
// records among it, and the result says older ones exist: at the page
// bound, on a page that fails, on a cursor seen before, on one too long.
func TestHistoryThatStopsEarlyKeepsTheNewestAndSaysSo(t *testing.T) {
	plain := func(o int) string { return fmt.Sprintf("c%d", o) }
	for name, tc := range map[string]struct {
		route *pagedRoute
		pages int
		want  string
	}{
		"the page bound":       {&pagedRoute{records: numbered(8), size: 3, cursor: plain}, 2, "3,4,5,6,7,8"},
		"a page that fails":    {&pagedRoute{records: numbered(8), size: 3, cursor: plain, fail: map[int]bool{3: true}}, 0, "6,7,8"},
		"a cursor seen before": {&pagedRoute{records: numbered(8), size: 3, cursor: func(int) string { return "same" }}, 0, "6,7,8,6,7,8"},
		"a cursor too long":    {&pagedRoute{records: numbered(8), size: 3, cursor: func(o int) string { return strings.Repeat("c", historyCursorMax+1) + strconv.Itoa(o) }}, 0, "6,7,8"},
	} {
		e, s := pagedEngine(t, tc.route)
		events, partial, err := e.readHistory(context.Background(), s, pagedHistory(tc.pages), "A")
		got := order(events)
		if name == "a cursor seen before" {
			// The same page came back once more; its records are not
			// replayed twice.
			tc.want = "6,7,8"
		}
		if err != nil || !partial || got != tc.want {
			t.Errorf("%s: history = %s partial %v err %v, want %s and partial", name, got, partial, err, tc.want)
		}
	}
	// Only a first page that cannot be read is an error.
	e, s := pagedEngine(t, &pagedRoute{records: numbered(8), size: 3, cursor: plain, fail: map[int]bool{0: true}})
	if events, _, err := e.readHistory(context.Background(), s, pagedHistory(0), "A"); err == nil || len(events) != 0 {
		t.Errorf("a first page that failed gave %d records and %v", len(events), err)
	}
}

// Pages that overlap list a record twice: it is replayed once, in its
// place. An empty page that still names an older one is passed over. More
// records than are kept lose the oldest, and that is said.
func TestHistoryPagesOverlapEmptyAndOverflow(t *testing.T) {
	plain := func(o int) string { return fmt.Sprintf("c%d", o) }
	e, s := pagedEngine(t, &pagedRoute{records: numbered(8), size: 3, cursor: plain, overlap: 1})
	if events, partial, err := e.readHistory(context.Background(), s, pagedHistory(0), "A"); err != nil || partial || order(events) != "1,2,3,4,5,6,7,8" {
		t.Errorf("overlapping pages = %s partial %v err %v", order(events), partial, err)
	}
	e, s = pagedEngine(t, &pagedRoute{records: numbered(8), size: 3, cursor: plain, empty: map[int]bool{3: true}})
	if events, partial, err := e.readHistory(context.Background(), s, pagedHistory(0), "A"); err != nil || partial || order(events) != "1,2,6,7,8" {
		t.Errorf("an empty page in the middle = %s partial %v err %v", order(events), partial, err)
	}
	e, s = pagedEngine(t, &pagedRoute{records: numbered(maxHistory + 40), size: 200, cursor: plain})
	h := pagedHistory(0)
	h.Path = "/log/{{.conversation}}?limit=200&order=desc"
	events, partial, err := e.readHistory(context.Background(), s, h, "A")
	if err != nil || !partial || len(events) != maxHistory {
		t.Fatalf("overflow: %d records partial %v err %v", len(events), partial, err)
	}
	var newest struct {
		N int `json:"n"`
	}
	_ = json.Unmarshal(events[len(events)-1], &newest)
	if newest.N != maxHistory+40 {
		t.Errorf("the newest record kept is %d, want %d", newest.N, maxHistory+40)
	}
}

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
// first, under one fixed line saying earlier messages are in the terminal.
// The line names no error and no cursor.
func TestAChatWithOlderUnreadMessagesSaysSoAboveThem(t *testing.T) {
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
	want := []string{"NOTICE:" + olderInTerminal, "said 2", "said 3", "said 4", "said 5"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chat = %q, want %q", got, want)
	}
	if strings.Contains(strings.Join(got, "|"), "secret-cursor") {
		t.Error("the notice repeats the cursor")
	}
}
