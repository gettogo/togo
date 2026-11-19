package enginetest

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// JSON-RPC error codes the test server answers with.
const (
	codexMethodNotFound = -32601
	codexInvalid        = -32600
	answered            = 1 // the call was answered already
)

// codexModels is the model list the server answers model/list with: the
// payload quoted in the adapter's tests (adapters/codex/efforts_test.go:20).
// No recording holds a model/list answer.
const codexModels = `{"data":[
		{"id":"gpt-a","isDefault":false,"supportedReasoningEfforts":[{"reasoningEffort":"low"}]},
		{"id":"gpt-b","isDefault":true,"supportedReasoningEfforts":[{"reasoningEffort":"medium"},{"reasoningEffort":"high"}]}]}`

func (s *CodexServer) accept(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(16 << 20)
	s.mu.Lock()
	s.seq++
	c := &codexConn{n: s.seq, c: ws, out: make(chan []byte, 4096), subs: map[string]bool{}, closed: make(chan struct{})}
	if s.srv == nil {
		s.mu.Unlock()
		_ = ws.CloseNow()
		return
	}
	s.conns[c] = true
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			select {
			case <-c.closed:
				return
			case b := <-c.out:
				if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
					c.close()
					return
				}
			}
		}
	}()
	defer func() {
		c.close()
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
	}()
	for {
		_, b, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var m codexWire
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		switch {
		case m.Method == "" && len(m.ID) > 0:
			s.reply(c, m)
		case m.Method == "initialized":
			s.mu.Lock()
			c.ready = true
			s.mu.Unlock()
		case m.request():
			go s.call(c, m)
		}
	}
}

// reply takes a client's answer to a server request: the first answer
// wins, and the server says the request is resolved.
func (s *CodexServer) reply(c *codexConn, m codexWire) {
	key := requestKey(m.ID)
	s.mu.Lock()
	req, open := s.fold.pending[key]
	s.replies = append(s.replies, CodexCall{Conn: c.n, Method: req.Method, Params: m.Result, At: time.Now()})
	on := s.OnReply
	s.mu.Unlock()
	if !open {
		return
	}
	var id any
	_ = json.Unmarshal(m.ID, &id)
	s.Append(sessiongen.CodexResolved(req.Thread, id))
	s.Flush()
	if on != nil {
		on(req, m.Result)
	}
}

// call answers one call, after any delay or failure it was told to give.
func (s *CodexServer) call(c *codexConn, m codexWire) {
	s.mu.Lock()
	s.calls = append(s.calls, CodexCall{Conn: c.n, Method: m.Method, Params: m.Params, At: time.Now()})
	delay, hang := s.slow[m.Method], s.hang[m.Method]
	var fail *codexFail
	if f := s.fail[m.Method]; f != nil && f.left != 0 {
		fail = f
		if f.left > 0 {
			f.left--
		}
	}
	s.mu.Unlock()
	if hang != nil {
		select {
		case <-hang:
		case <-c.closed:
			return
		}
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-c.closed:
			return
		}
	}
	if fail != nil {
		c.answer(m.ID, nil, fail.code, fail.message)
		return
	}
	res, code, msg := s.answer(c, m)
	if code == answered {
		return
	}
	c.answer(m.ID, res, code, msg)
}

func (c *codexConn) answer(id json.RawMessage, res any, code int, message string) {
	out := map[string]any{"jsonrpc": "2.0", "id": id}
	if code != 0 {
		out["error"] = map[string]any{"code": code, "message": message}
	} else {
		out["result"] = res
	}
	b, err := json.Marshal(out)
	if err == nil {
		c.send(b)
	}
}

// answer is the result of a call. Results the helper does not read are
// empty objects: no recording holds them.
func (s *CodexServer) answer(c *codexConn, m codexWire) (any, int, string) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		KeyPath  string `json:"keyPath"`
		Value    any    `json:"value"`
	}
	_ = json.Unmarshal(m.Params, &p)
	switch m.Method {
	case "initialize":
		return map[string]any{}, 0, ""
	case "account/rateLimits/read":
		s.mu.Lock()
		limits := s.fold.limits
		s.mu.Unlock()
		if limits == nil {
			return nil, codexInvalid, "not signed in"
		}
		return limits, 0, ""
	case "thread/loaded/list":
		return map[string]any{"data": s.Loaded()}, 0, ""
	case "thread/resume":
		// The result and the subscription are one step under the lock:
		// the thread's next message reaches the client after its result.
		s.mu.Lock()
		s.catchUpLocked()
		res, ok := s.fold.resume(p.ThreadID)
		if ok {
			s.fold.load(p.ThreadID)
			b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": res})
			if err == nil {
				c.subs[p.ThreadID] = true
				c.send(b)
			}
		}
		s.mu.Unlock()
		if !ok {
			return nil, codexInvalid, "no rollout found for thread id " + p.ThreadID
		}
		return nil, answered, ""
	case "model/list":
		return json.RawMessage(codexModels), 0, ""
	case "turn/interrupt":
		s.mu.Lock()
		on := s.OnInterrupt
		s.mu.Unlock()
		if on != nil {
			go on(p.ThreadID, p.TurnID)
		}
		return map[string]any{}, 0, ""
	case "config/value/write":
		if p.KeyPath == "approval_policy" {
			s.mu.Lock()
			if v, ok := p.Value.(string); ok {
				s.fold.policy = v
			}
			s.mu.Unlock()
		}
		return map[string]any{}, 0, ""
	case "thread/compact/start":
		return map[string]any{}, 0, ""
	}
	return nil, codexMethodNotFound, "method not found"
}
