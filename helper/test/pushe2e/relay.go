package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gettogo/togo/helper/internal/seal"
)

// relayPush is the part of the helper's POST /v1/push body the local relay
// reads.
type relayPush struct {
	Token      string `json:"token"`
	Ciphertext string `json:"ciphertext"`
	CollapseID string `json:"collapseId"`
	Kind       string `json:"kind"`
}

// pushed is one push the local relay received.
type pushed struct {
	Kind       string
	Ciphertext string
	// File is the .apns file written for it, empty for Live Activity
	// pushes, which simctl cannot deliver.
	File string
}

type mail struct {
	id         string
	ciphertext string
	receivedAt int64
}

// localRelay stands in for the hosted relay: pushes become .apns files in
// dir (the payload the relay would hand APNs), and the mailbox works as the
// hosted one does, so a phone's answer reaches the helper with no network.
type localRelay struct {
	dir string
	// forbid are strings that must never appear in a request body.
	forbid []string

	mu        sync.Mutex
	pushes    []pushed
	plaintext []string
	queue     map[string][]mail // host id -> unacked messages
	conns     map[string]*websocket.Conn
	posted    int
	acked     int
	seq       int
	changed   chan struct{}
	// onChange runs with the lock held after every change.
	onChange func()
}

func newLocalRelay(dir string, forbid []string) *localRelay {
	return &localRelay{dir: dir, forbid: forbid, queue: map[string][]mail{}, conns: map[string]*websocket.Conn{}, changed: make(chan struct{}, 1)}
}

func (r *localRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch p := req.URL.Path; {
	case p == "/v1/push" && req.Method == http.MethodPost:
		r.push(w, req)
	case strings.HasPrefix(p, "/v1/mailbox/") && strings.HasSuffix(p, "/ws"):
		r.connect(w, req, strings.TrimSuffix(strings.TrimPrefix(p, "/v1/mailbox/"), "/ws"))
	case strings.HasPrefix(p, "/v1/mailbox/") && req.Method == http.MethodPost:
		r.post(w, req, strings.TrimPrefix(p, "/v1/mailbox/"))
	default:
		http.NotFound(w, req)
	}
}

// apnsPayload is what the hosted relay sends APNs for kind: a generic alert
// the notification service replaces, or a silent push; c is the envelope.
func apnsPayload(kind, ciphertext string) ([]byte, bool) {
	var aps map[string]any
	switch kind {
	case "alert":
		aps = map[string]any{"alert": map[string]string{"title": "Togo", "body": "Needs you"}, "mutable-content": 1, "sound": "default"}
	case "background":
		aps = map[string]any{"content-available": 1}
	default:
		return nil, false
	}
	raw, err := json.Marshal(map[string]any{"aps": aps, "c": ciphertext})
	return raw, err == nil
}

func (r *localRelay) push(w http.ResponseWriter, req *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(req.Body, 16<<10))
	var body relayPush
	if err != nil || json.Unmarshal(raw, &body) != nil {
		http.Error(w, `{"error":"bad_request"}`, http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.forbid {
		if s != "" && strings.Contains(string(raw), s) {
			r.plaintext = append(r.plaintext, s)
		}
	}
	p := pushed{Kind: body.Kind, Ciphertext: body.Ciphertext}
	if payload, ok := apnsPayload(body.Kind, body.Ciphertext); ok && r.dir != "" {
		r.seq++
		p.File = filepath.Join(r.dir, fmt.Sprintf("push-%03d-%s.apns", r.seq, body.Kind))
		if err := os.WriteFile(p.File, payload, 0o600); err != nil {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
	}
	r.pushes = append(r.pushes, p)
	r.notify()
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{}`))
}

func (r *localRelay) post(w http.ResponseWriter, req *http.Request, hostID string) {
	var body struct {
		Ciphertext string `json:"ciphertext"`
	}
	if json.NewDecoder(io.LimitReader(req.Body, 6144)).Decode(&body) != nil || body.Ciphertext == "" {
		http.Error(w, `{"error":"bad_request","field":"ciphertext"}`, http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	r.seq++
	m := mail{id: fmt.Sprintf("M%025d", r.seq), ciphertext: body.Ciphertext, receivedAt: time.Now().UnixMilli()}
	r.queue[hostID] = append(r.queue[hostID], m)
	r.posted++
	conn := r.conns[hostID]
	r.notify()
	r.mu.Unlock()
	if conn != nil {
		sendMail(req.Context(), conn, m)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, `{"id":%q}`, m.id)
}

// connect is the helper's mailbox socket. As on the hosted relay, the
// bearer secret must hash to the host id, and a newer socket replaces the
// older one.
func (r *localRelay) connect(w http.ResponseWriter, req *http.Request, hostID string) {
	secret, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "))
	if err != nil || seal.HostID(secret) != hostID {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, req, nil)
	if err != nil {
		return
	}
	ctx := req.Context()
	r.mu.Lock()
	if old := r.conns[hostID]; old != nil {
		old.Close(4000, "replaced")
	}
	r.conns[hostID] = conn
	backlog := append([]mail(nil), r.queue[hostID]...)
	r.mu.Unlock()
	for _, m := range backlog {
		sendMail(ctx, conn, m)
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			r.mu.Lock()
			if r.conns[hostID] == conn {
				delete(r.conns, hostID)
			}
			r.mu.Unlock()
			return
		}
		var f struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch f.Type {
		case "ping":
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"pong"}`))
		case "ack":
			r.ack(hostID, f.ID)
		}
	}
}

func (r *localRelay) ack(hostID, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.queue[hostID]
	for i, m := range q {
		if m.id == id {
			r.queue[hostID] = append(q[:i:i], q[i+1:]...)
			r.acked++
			r.notify()
			return
		}
	}
}

func sendMail(ctx context.Context, conn *websocket.Conn, m mail) {
	frame, _ := json.Marshal(map[string]any{"type": "msg", "id": m.id, "ciphertext": m.ciphertext, "receivedAt": m.receivedAt})
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = conn.Write(wctx, websocket.MessageText, frame)
}

// notify wakes a waiter; r.mu is held.
func (r *localRelay) notify() {
	if r.onChange != nil {
		r.onChange()
	}
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

// waitFor polls cond, under the lock, until it holds or d passes.
func (r *localRelay) waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		r.mu.Lock()
		ok := cond()
		r.mu.Unlock()
		if ok {
			return true
		}
		left := time.Until(deadline)
		if left <= 0 {
			return false
		}
		select {
		case <-r.changed:
		case <-time.After(min(left, 200*time.Millisecond)):
		}
	}
}

// pushesOf returns the pushes of kind so far; r.mu is held.
func (r *localRelay) pushesOf(kind string) []pushed {
	var out []pushed
	for _, p := range r.pushes {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out
}
