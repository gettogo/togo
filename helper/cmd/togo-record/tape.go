package main

import (
	"encoding/json"
	"sync"
	"time"
)

// line is one record of a replay fixture after the header.
type line struct {
	// T is milliseconds since the recording began.
	T int64 `json:"t"`
	// Dir is "out" for what the app sent, "in" for what the helper sent.
	Dir string `json:"dir"`
	// Method names the request a response answers, or the notification.
	Method string          `json:"method"`
	Frame  json.RawMessage `json:"frame"`
}

// tape keeps the frames that concern the recorded session and tracks its
// state, so the scenario knows when to answer, interrupt or stop.
//
// Event notifications for other sessions are dropped and the rest are
// renumbered from the snapshot's seq, so the fixture reads as if this were
// the machine's only session. Notifications that arrive before the session
// id is known wait until it is.
type tape struct {
	mu      sync.Mutex
	began   time.Time
	session string
	lines   []line
	held    []line
	baseSeq int64
	kept    int64
	last    time.Time

	items     map[string]map[string]any
	approvals map[string]map[string]any
	status    string
	working   bool
	sawWork   bool
	gone      bool
}

func newTape() *tape {
	return &tape{began: time.Now(), last: time.Now(), items: map[string]map[string]any{},
		approvals: map[string]map[string]any{}}
}

func (t *tape) onFrame(dir, method string, raw []byte) {
	if method == "events.keepalive" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	l := line{T: time.Since(t.began).Milliseconds(), Dir: dir, Method: method, Frame: append(json.RawMessage(nil), raw...)}
	t.last = time.Now()
	switch {
	case dir == "in" && (method == "event" || method == "harness"):
		if t.session == "" {
			t.held = append(t.held, l)
			return
		}
		t.keep(l)
	case dir == "in" && method == "events.subscribe":
		t.lines = append(t.lines, t.snapshot(l))
	case dir == "in" && method == "harness.open":
		t.lines = append(t.lines, l)
		var f struct {
			Result struct {
				Items   []map[string]any `json:"items"`
				Mission struct {
					Summary struct {
						Working bool `json:"working"`
					} `json:"summary"`
				} `json:"mission"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &f) == nil {
			for _, it := range f.Result.Items {
				t.items[str(it["id"])] = it
			}
			t.setWorking(f.Result.Mission.Summary.Working)
		}
	default:
		t.lines = append(t.lines, l)
	}
}

// adopt names the recorded session and files what was held for it.
func (t *tape) adopt(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.session = id
	held := t.held
	t.held = nil
	for _, l := range held {
		t.keep(l)
	}
}

func (t *tape) keep(l line) {
	var f struct {
		Params map[string]any `json:"params"`
	}
	if json.Unmarshal(l.Frame, &f) != nil || f.Params == nil || str(f.Params["sessionId"]) != t.session {
		return
	}
	if l.Method == "event" {
		t.kept++
		f.Params["seq"] = t.baseSeq + t.kept
		l.Frame, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "event", "params": f.Params})
		t.trackEvent(f.Params)
	} else {
		t.trackHarness(f.Params)
	}
	t.lines = append(t.lines, l)
}

// snapshot keeps only this session's rows of the events.subscribe answer.
// The session does not exist yet, so in practice both lists come out empty.
func (t *tape) snapshot(l line) line {
	var f map[string]any
	if json.Unmarshal(l.Frame, &f) != nil {
		return l
	}
	res, _ := f["result"].(map[string]any)
	if res == nil {
		return l
	}
	if n, ok := res["seq"].(float64); ok {
		t.baseSeq = int64(n)
	}
	res["sessions"] = []any{}
	res["approvals"] = []any{}
	l.Frame, _ = json.Marshal(f)
	return l
}

func (t *tape) trackEvent(p map[string]any) {
	switch str(p["kind"]) {
	case "session":
		s, _ := p["session"].(map[string]any)
		t.status = str(s["status"])
		if t.status == "working" {
			t.sawWork = true
		}
		if t.status == "exited" || t.status == "ended" || t.status == "failed" {
			t.gone = true
		}
	case "session.removed":
		t.gone = true
	case "approval.open":
		a, _ := p["approval"].(map[string]any)
		t.approvals[str(a["requestId"])] = a
	case "approval.closed":
		delete(t.approvals, str(p["requestId"]))
	}
}

func (t *tape) trackHarness(p map[string]any) {
	if items, ok := p["items"].([]any); ok {
		for _, raw := range items {
			if it, ok := raw.(map[string]any); ok {
				t.items[str(it["id"])] = it
			}
		}
	}
	if m, ok := p["mission"].(map[string]any); ok {
		ops, _ := m["ops"].([]any)
		for _, raw := range ops {
			op, _ := raw.(map[string]any)
			if str(op["op"]) != "summary" {
				continue
			}
			if s, ok := op["summary"].(map[string]any); ok {
				w, _ := s["working"].(bool)
				t.setWorking(w)
			}
		}
	}
}

func (t *tape) setWorking(w bool) {
	t.working = w
	if w {
		t.sawWork = true
	}
}

// view is what the scenario reads.
type view struct {
	busy, sawWork, gone bool
	quiet               time.Duration
	approvals           []map[string]any
	questions           []map[string]any
}

func (t *tape) view() view {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := view{busy: t.working || t.status == "working", sawWork: t.sawWork, gone: t.gone, quiet: time.Since(t.last)}
	for _, a := range t.approvals {
		v.approvals = append(v.approvals, a)
	}
	for _, it := range t.items {
		if str(it["kind"]) != "question" {
			continue
		}
		q, _ := it["question"].(map[string]any)
		if ans, _ := q["answered"].(map[string]any); len(ans) == 0 {
			v.questions = append(v.questions, it)
		}
	}
	return v
}

func (t *tape) snapshotLines() []line {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]line(nil), t.lines...)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
