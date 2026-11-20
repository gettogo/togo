package enginetest

import (
	"encoding/json"
	"slices"
)

// codexFold is the test app-server's memory of its threads, built from
// the journal as the real server builds it from its rollouts: each thread
// with its settings and its turns, each turn with its items in order, the
// threads loaded now, and the server requests not yet answered.
type codexFold struct {
	threads map[string]*foldThread
	order   []string
	loaded  []string
	pending map[string]codexRequest // by request key
	limits  json.RawMessage         // the newest account/rateLimits/updated params
	cwd     string
	policy  string // the approval policy a new thread takes
}

type foldThread struct {
	obj                   map[string]any
	status                string
	model, effort, policy string
	turns                 []*foldTurn
}

type foldTurn struct {
	obj   map[string]any
	items []string
	byID  map[string]map[string]any
}

// codexRequest is a server request the server waits on.
type codexRequest struct {
	ID     json.RawMessage
	Method string
	Thread string
	Turn   string
}

func newCodexFold(cwd, policy string) *codexFold {
	return &codexFold{threads: map[string]*foldThread{}, pending: map[string]codexRequest{}, cwd: cwd, policy: policy}
}

// codexWire is one message as the journal and the socket carry it.
type codexWire struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

func (m codexWire) request() bool { return m.Method != "" && len(m.ID) > 0 && string(m.ID) != "null" }

// threadOf is the thread a message is about, "" for the account's.
func (m codexWire) threadOf() string {
	var p struct {
		ThreadID string `json:"threadId"`
		Thread   struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	_ = json.Unmarshal(m.Params, &p)
	if p.ThreadID != "" {
		return p.ThreadID
	}
	return p.Thread.ID
}

func (f *codexFold) load(id string) {
	if !slices.Contains(f.loaded, id) {
		f.loaded = append(f.loaded, id)
	}
}

func (f *codexFold) unload(id string) {
	f.loaded = slices.DeleteFunc(f.loaded, func(s string) bool { return s == id })
}

// apply takes one journal message into the fold.
func (f *codexFold) apply(m codexWire) {
	var p map[string]any
	_ = json.Unmarshal(m.Params, &p)
	th := f.threads[m.threadOf()]
	if m.request() {
		turn, _ := p["turnId"].(string)
		f.pending[requestKey(m.ID)] = codexRequest{ID: m.ID, Method: m.Method, Thread: m.threadOf(), Turn: turn}
		return
	}
	switch m.Method {
	case "thread/started":
		obj, _ := p["thread"].(map[string]any)
		id, _ := obj["id"].(string)
		if id == "" {
			return
		}
		model, _ := obj["model"].(string)
		effort, _ := obj["reasoningEffort"].(string)
		f.threads[id] = &foldThread{obj: obj, status: "idle", model: model, effort: effort, policy: f.policy}
		f.order = append(f.order, id)
		f.load(id)
	case "thread/status/changed":
		if th == nil {
			return
		}
		st, _ := p["status"].(map[string]any)
		th.status, _ = st["type"].(string)
		if th.status == "notLoaded" {
			f.unload(m.threadOf())
		}
	case "thread/settings/updated":
		if th == nil {
			return
		}
		s, _ := p["threadSettings"].(map[string]any)
		if v, ok := s["model"].(string); ok {
			th.model = v
		}
		if v, ok := s["effort"].(string); ok {
			th.effort = v
		}
		if v, ok := s["approvalPolicy"].(string); ok {
			th.policy = v
		}
	case "turn/started", "turn/completed":
		if th == nil {
			return
		}
		obj, _ := p["turn"].(map[string]any)
		id, _ := obj["id"].(string)
		if t := th.turn(id); t != nil {
			for _, k := range []string{"status", "error", "completedAt", "durationMs"} {
				t.obj[k] = obj[k]
			}
			return
		}
		th.turns = append(th.turns, &foldTurn{obj: obj, byID: map[string]map[string]any{}})
	case "item/started", "item/completed":
		if th == nil {
			return
		}
		turn, _ := p["turnId"].(string)
		it, _ := p["item"].(map[string]any)
		id, _ := it["id"].(string)
		t := th.turn(turn)
		if t == nil || id == "" {
			return
		}
		if _, ok := t.byID[id]; !ok {
			t.items = append(t.items, id)
		}
		t.byID[id] = it
	case "serverRequest/resolved":
		var r struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		_ = json.Unmarshal(m.Params, &r)
		delete(f.pending, requestKey(r.RequestID))
	case "account/rateLimits/updated":
		f.limits = append(json.RawMessage(nil), m.Params...)
	}
}

func (th *foldThread) turn(id string) *foldTurn {
	for _, t := range th.turns {
		if t.obj["id"] == id {
			return t
		}
	}
	return nil
}

// resume is a thread/resume result: the thread with every turn and its
// items, and the thread's settings (appserver-0167.jsonl:1; the
// "paginated" history still returns every turn's items).
func (f *codexFold) resume(id string) (map[string]any, bool) {
	th := f.threads[id]
	if th == nil {
		return nil, false
	}
	obj := make(map[string]any, len(th.obj)+2)
	for k, v := range th.obj {
		obj[k] = v
	}
	status := map[string]any{"type": th.status}
	if th.status == "active" {
		status["activeFlags"] = []any{}
	}
	obj["status"] = status
	turns := make([]any, 0, len(th.turns))
	for _, t := range th.turns {
		items := make([]any, 0, len(t.items))
		for _, i := range t.items {
			items = append(items, t.byID[i])
		}
		turns = append(turns, map[string]any{"id": t.obj["id"], "items": items, "itemsView": "full", "status": t.obj["status"],
			"error": t.obj["error"], "startedAt": t.obj["startedAt"], "completedAt": t.obj["completedAt"], "durationMs": t.obj["durationMs"]})
	}
	obj["turns"] = turns
	cwd, _ := th.obj["cwd"].(string)
	if cwd == "" {
		cwd = f.cwd
	}
	return map[string]any{"thread": obj, "model": th.model, "cwd": cwd, "reasoningEffort": th.effort, "approvalPolicy": th.policy}, true
}

// running is the open turn of a thread and its items still in progress.
func (f *codexFold) running(thread string) (string, []map[string]any) {
	th := f.threads[thread]
	if th == nil {
		return "", nil
	}
	for i := len(th.turns) - 1; i >= 0; i-- {
		t := th.turns[i]
		if t.obj["status"] != "inProgress" {
			continue
		}
		var open []map[string]any
		for _, id := range t.items {
			if it := t.byID[id]; it["status"] == "inProgress" {
				open = append(open, it)
			}
		}
		id, _ := t.obj["id"].(string)
		return id, open
	}
	return "", nil
}

// main is the newest top-level thread loaded: the one the terminal shows.
func (f *codexFold) main() string {
	for i := len(f.loaded) - 1; i >= 0; i-- {
		if th := f.threads[f.loaded[i]]; th != nil && th.obj["parentThreadId"] == nil {
			return f.loaded[i]
		}
	}
	return ""
}

// requestKey names a request id as the helper's runner does: "rq-" and
// the id's text.
func requestKey(id json.RawMessage) string {
	var s string
	if json.Unmarshal(id, &s) == nil {
		return "rq-" + s
	}
	return "rq-" + string(id)
}
