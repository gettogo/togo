package httpsrc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestParserFields(t *testing.T) {
	in := ": comment\r\n" +
		"event: note\r\nid: 7\r\ndata: line one\r\ndata: line two\r\n\r\n" +
		"retry: 1500\ndata:{\"a\":1}\n\n" +
		"id: 8\n\n" + // an event with no data is not dispatched
		"data: cr only\r\r" +
		"data: unterminated"
	p := NewParser(strings.NewReader(in))
	ev, err := p.Next()
	if err != nil || ev.Event != "note" || ev.ID != "7" || ev.Data != "line one\nline two" {
		t.Fatalf("first: %+v %v", ev, err)
	}
	ev, err = p.Next()
	if err != nil || ev.Data != `{"a":1}` || ev.Retry != 1500*time.Millisecond {
		t.Fatalf("second: %+v %v", ev, err)
	}
	ev, err = p.Next()
	if err != nil || ev.Data != "cr only" || p.LastID != "8" {
		t.Fatalf("third: %+v %v last %q", ev, err, p.LastID)
	}
	if _, err = p.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("unterminated event must not dispatch: %v", err)
	}
}

func TestParserEventCap(t *testing.T) {
	big := "data: " + strings.Repeat("x", MaxEvent/2) + "\n"
	p := NewParser(strings.NewReader(big + big + big + "\n"))
	if _, err := p.Next(); err == nil {
		t.Fatal("oversized event accepted")
	}
}

func TestEventDataScrubbedAndWrapped(t *testing.T) {
	d := eventData(Event{Data: `{"type":"x","properties":{"apiKey":"v","text":"sk-abcdefghijklmnopqrst"}}`})
	if strings.Contains(string(d), `"v"`) || strings.Contains(string(d), "sk-abc") {
		t.Fatalf("not scrubbed: %s", d)
	}
	var w map[string]string
	if err := json.Unmarshal(eventData(Event{Event: "ping", Data: "hello"}), &w); err != nil || w["event"] != "ping" || w["data"] != "hello" {
		t.Fatalf("wrap: %v %v", w, err)
	}
}

// The feed reconnects after the server drops it and resumes with the last
// event id it saw.
func TestWatchReconnectsWithLastEventID(t *testing.T) {
	var mu sync.Mutex
	var seenIDs []string
	conns := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/event" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		conns++
		n := conns
		seenIDs = append(seenIDs, r.Header.Get("Last-Event-ID"))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "retry: 10\nid: e%d\ndata: {\"n\":%d}\n\n", n, n)
		w.(http.Flusher).Flush()
		// return: the stream ends and the client must come back
	}))
	defer srv.Close()
	raw, _ := json.Marshal(spec.HTTP{Path: "/event"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := make(chan int, 10)
	var reconnects atomic.Int32
	go SSE{}.Watch(ctx, harness.SourceSpec{Name: "events", Kind: spec.KindSSE, Raw: raw}, envFor(srv), func(r harness.Record) {
		var v struct {
			N    int
			Type string
		}
		json.Unmarshal(r.Data, &v)
		if v.Type == Reconnected {
			reconnects.Add(1)
			return
		}
		got <- v.N
	})
	for want := 1; want <= 3; want++ {
		select {
		case n := <-got:
			if n != want {
				t.Fatalf("event %d, want %d", n, want)
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for reconnect")
		}
	}
	cancel()
	if n := reconnects.Load(); n < 2 {
		t.Fatalf("%d reconnect records, want one per reconnect", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if seenIDs[0] != "" || seenIDs[1] != "e1" || seenIDs[2] != "e2" {
		t.Fatalf("Last-Event-ID sequence %v", seenIDs)
	}
}

func TestWatchRefusesRemote(t *testing.T) {
	raw, _ := json.Marshal(spec.HTTP{Path: "/event"})
	err := SSE{}.Watch(context.Background(), harness.SourceSpec{Raw: raw}, harness.Env{Endpoint: "http://192.0.2.1:1"}, func(harness.Record) {})
	if !errors.Is(err, ErrNotLoopback) {
		t.Fatalf("remote: %v", err)
	}
}

// Requests to a password-protected harness carry its Authorization.
func TestRequestsCarryAuth(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	env := envFor(srv)
	env.Auth = "Basic abc"
	if _, err := Do(context.Background(), env, spec.HTTP{Path: "/x"}); err != nil {
		t.Fatal(err)
	}
	if a := <-got; a != "Basic abc" {
		t.Fatalf("authorization %q", a)
	}
}

// A retry field on an event with no data still sets the reconnect delay.
func TestRetryWithoutData(t *testing.T) {
	p := NewParser(strings.NewReader("retry: 50\n\ndata: {}\n\n"))
	if _, err := p.Next(); err != nil {
		t.Fatal(err)
	}
	if p.Retry != 50*time.Millisecond {
		t.Fatalf("retry %v", p.Retry)
	}
}
