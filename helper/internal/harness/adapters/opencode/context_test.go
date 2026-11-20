package opencode

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The recorded session ran local/qwen3.8-27b, which the recorded provider
// list (another capture) does not carry. These tests replay the session as
// if it ran local/coder-dense, which the list gives a 65,536-token window.
func asCoderDense(recs []harness.Record) []harness.Record {
	out := make([]harness.Record, len(recs))
	for i, r := range recs {
		r.Data = bytes.ReplaceAll(r.Data, []byte(`"modelID": "qwen3.8-27b"`), []byte(`"modelID": "coder-dense"`))
		out[i] = r
	}
	return out
}

// providersServer answers /config/providers with the recorded list and
// counts the calls.
func providersServer(t *testing.T) (harness.Env, *atomic.Int32) {
	t.Helper()
	raw, err := os.ReadFile("testdata/providers.json")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/providers" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return harness.Env{SessionID: "s_oc", Endpoint: srv.URL}, &calls
}

// contextOf is the context percent the ops set, if any.
func contextOf(ops []harness.Op) *float64 {
	var got *float64
	for _, op := range ops {
		if op.Op == harness.OpStatus && op.Status != nil && op.Status.ContextPercent != nil {
			got = op.Status.ContextPercent
		}
	}
	return got
}

// waitWoken waits for the mapper's lookup to wake the engine and returns
// what Woken publishes.
func waitWoken(t *testing.T, m *Events, woke chan struct{}) []harness.Op {
	t.Helper()
	select {
	case <-woke:
	case <-time.After(10 * time.Second):
		t.Fatal("the model list lookup never woke the engine")
	}
	_, ops := m.Woken()
	return ops
}

func wakeChan(m *Events) chan struct{} {
	woke := make(chan struct{}, 8)
	m.Wake(func() { woke <- struct{}{} })
	return woke
}

// The status shows how full the model's window is after each step, from
// the latest assistant message's tokens and the window the provider list
// gives the model. The list is asked once.
func TestContextPercentFromRecordedSession(t *testing.T) {
	env, calls := providersServer(t)
	m := NewEvents(env)
	woke := wakeChan(m)
	var seen []float64
	note := func(ops []harness.Op) {
		if p := contextOf(ops); p != nil {
			seen = append(seen, *p)
		}
	}
	woken := false
	for _, r := range asCoderDense(recorded(t)) {
		_, ops := m.Map(r)
		note(ops)
		select {
		case <-woke:
			woken = true
			_, ops := m.Woken()
			note(ops)
		default:
		}
	}
	if !woken {
		note(waitWoken(t, m, woke))
	}
	// Steps from 19,637 tokens (30.0 %) to the last, 77 input, 25 output
	// and 20,698 cached: 20,800 of 65,536.
	if len(seen) == 0 || seen[len(seen)-1] != 31.7 {
		t.Fatalf("percents shown: %v, want the last at 31.7", seen)
	}
	for i, p := range seen {
		if p < 30.0 || p > 31.7 || i > 0 && p < seen[i-1] {
			t.Fatalf("percents shown: %v", seen)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("the provider list was read %d times", n)
	}
}

// A model the provider list gives no window shows no percent: nothing is
// guessed.
func TestContextPercentUnknownWindow(t *testing.T) {
	env, calls := providersServer(t)
	m := NewEvents(env)
	woke := wakeChan(m)
	woken := false
	for _, r := range recorded(t) {
		_, ops := m.Map(r)
		if p := contextOf(ops); p != nil {
			t.Fatalf("a percent for a model with no known window: %v", *p)
		}
		select {
		case <-woke:
			woken = true
			if _, ops := m.Woken(); contextOf(ops) != nil {
				t.Fatalf("a percent for a model with no known window: %v", *contextOf(ops))
			}
		default:
		}
	}
	if !woken {
		if p := contextOf(waitWoken(t, m, woke)); p != nil {
			t.Fatalf("a percent for a model with no known window: %v", *p)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("the provider list was read %d times", n)
	}
}

func assistant(sess, id string, created int64, model string, input, output, cacheRead int) harness.Record {
	b, _ := json.Marshal(map[string]any{"type": "message.updated", "properties": map[string]any{"sessionID": sess, "info": map[string]any{
		"id": id, "role": "assistant", "sessionID": sess, "parentID": "msg_user", "modelID": model, "providerID": "local",
		"time": map[string]any{"created": created, "completed": created + 1}, "finish": "stop",
		"tokens": map[string]any{"total": input + output + cacheRead, "input": input, "output": output, "reasoning": 0, "cache": map[string]any{"write": 0, "read": cacheRead}}}}})
	return harness.Record{Source: "events", Data: b}
}

func selectSession(id string) harness.Record {
	return harness.Record{Source: "events", Data: json.RawMessage(`{"type":"tui.session.select","properties":{"sessionID":"` + id + `"}}`)}
}

// The percent follows the latest message, not a sum: a smaller context
// after a compaction lowers it, and an older message read late does not
// raise it again. A subagent's messages do not count, and another
// conversation does not show the last one's.
func TestContextPercentLatestMainMessage(t *testing.T) {
	env, _ := providersServer(t)
	m := NewEvents(env)
	woke := wakeChan(m)
	m.Map(selectSession("ses_main"))
	m.Map(assistant("ses_main", "msg_b", 2000, "coder-dense", 100, 100, 32568))
	if p := contextOf(waitWoken(t, m, woke)); p == nil || *p != 50.0 {
		t.Fatalf("first: %v", p)
	}
	_, ops := m.Map(assistant("ses_main", "msg_c", 3000, "coder-dense", 6000, 553, 0))
	if p := contextOf(ops); p == nil || *p != 10.0 {
		t.Fatalf("after a compaction: %v", p)
	}
	if _, ops = m.Map(assistant("ses_main", "msg_a", 1000, "coder-dense", 60000, 100, 0)); contextOf(ops) != nil {
		t.Fatalf("an older message read late changed it: %v", *contextOf(ops))
	}
	m.Map(harness.Record{Data: json.RawMessage(`{"type":"session.created","properties":{"info":{"id":"ses_kid","parentID":"ses_main","title":"t"}}}`)})
	if _, ops = m.Map(assistant("ses_kid", "msg_k", 4000, "coder-dense", 60000, 100, 0)); contextOf(ops) != nil {
		t.Fatalf("a subagent's message changed it: %v", *contextOf(ops))
	}
	// Another conversation shows only its own.
	m.Map(selectSession("ses_other"))
	if _, ops = m.Map(assistant("ses_other", "msg_o", 500, "coder-dense", 3000, 276, 0)); contextOf(ops) == nil || *contextOf(ops) != 5.0 {
		t.Fatalf("the next conversation: %v", contextOf(ops))
	}
}
