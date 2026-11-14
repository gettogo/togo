package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/session"
)

// An event feed tells what happens from now on. A feed that names where
// its conversation's past can be read (spec.Feed.History) has that past
// loaded once per conversation: when the chat first learns which
// conversation it shows, when it moves to another one, and after a helper
// restart, from the conversation it showed before (kept in
// $TOGO_HOME/run/harness/<session>.conversation).
//
// The past is replayed through a fresh mapper, so nothing the old one
// counted is counted twice. Live records of the feed that arrive while the
// past is read are held and mapped after it, so none is lost; a mapper
// keys its items by the harness's own ids, so one the past already holds
// is the same item again.

// historyTimeout bounds one history read.
const historyTimeout = 20 * time.Second

// maxHeld bounds the live records held during a history read.
const maxHeld = 5000

var conversationID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

type heldRecords struct {
	feed string
	recs []harness.Record
}

// heldForHistoryLocked keeps back a live record of the feed whose past is
// being read.
func (s *sess) heldForHistoryLocked(rec harness.Record) bool {
	if s.held == nil || s.held.feed != rec.Source {
		return false
	}
	if len(s.held.recs) < maxHeld {
		s.held.recs = append(s.held.recs, rec)
	}
	return true
}

// wantHistoryLocked starts loading the past of the conversation the chat
// shows, once per conversation.
func (s *sess) wantHistoryLocked(mapper, feed string) {
	f, ok := s.ad.Feeds[feed]
	if !ok || f.History == nil || s.conv == "" || s.hydrated == s.conv || s.held != nil {
		return
	}
	s.hydrated = s.conv
	go s.e.saveConversation(s.id, s.conv)
	s.loadHistoryLocked(mapper, feed, s.conv)
}

func (s *sess) loadHistoryLocked(mapper, feed, conv string) {
	if !conversationID.MatchString(conv) {
		return
	}
	s.held = &heldRecords{feed: feed}
	go s.e.hydrate(s, mapper, feed, conv, s.convGen)
}

// resumeHistory loads the past of the conversation the session showed
// before the helper restarted, when the adapter's feed has a history.
func (s *sess) resumeHistory() {
	conv := s.e.loadConversation(s.id)
	if conv == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conv != "" || s.hydrated != "" || s.held != nil {
		return
	}
	for _, name := range s.ad.feedOrder {
		if f := s.ad.Feeds[name]; f.History != nil {
			s.hydrated = conv
			s.loadHistoryLocked(f.Mapper, name, conv)
			return
		}
	}
}

func (e *Engine) hydrate(s *sess, mapper, feed, conv string, gen uint64) {
	ctx, cancel := context.WithTimeout(s.ctx, historyTimeout)
	events, err := e.readHistory(ctx, s, s.ad.Feeds[feed].History, conv)
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.held
	s.held = nil
	if s.ctx.Err() != nil {
		return
	}
	moved := s.convGen != gen || (s.conv != "" && s.conv != conv)
	if err != nil && !moved {
		e.cfg.Logf("harness: %s: the conversation's earlier messages could not be read: %v", s.ad.Adapter.Adapter, err)
		if s.hydrated == conv {
			s.hydrated = "" // asked again on the next record
		}
	}
	if err == nil && !moved && len(events) > 0 {
		s.mappers[mapper] = e.mapperFor(mapper, s.env)
		s.restoreLocked(s.ctx, mapper, harness.Record{Source: feed, At: e.cfg.Now(), Replay: events})
	}
	// Held records are mapped as they would have been, after the past.
	for _, rec := range heldOf(held) {
		s.recordLocked(mapper, rec)
	}
}

// maxHistory bounds the records one history read replays.
const maxHistory = 2000

// readHistory asks the harness for the conversation's past and returns it
// as the feed mapper's records, oldest first.
func (e *Engine) readHistory(ctx context.Context, s *sess, h *spec.History, conv string) ([]json.RawMessage, error) {
	r, ok := harness.LookupRunner(spec.KindHTTP)
	if !ok {
		return nil, errors.New("no http runner")
	}
	raw, err := json.Marshal(spec.HTTP{Method: "GET", Path: strings.ReplaceAll(h.Path, "{{.conversation}}", conv)})
	if err != nil {
		return nil, err
	}
	out, err := r.Fetch(ctx, harness.SourceSpec{Name: "history", Kind: spec.KindHTTP, Raw: raw}, s.env)
	if err != nil {
		return nil, err
	}
	v, err := decode(out)
	if err != nil {
		return nil, errors.New("history is not JSON")
	}
	if h.Items != "" {
		if v, err = JQFirst(h.Items, v, nil); err != nil {
			return nil, err
		}
	}
	list, _ := v.([]any)
	if len(list) > maxHistory {
		list = list[len(list)-maxHistory:]
	}
	events := make([]json.RawMessage, 0, len(list))
	for _, el := range list {
		var rec any = el
		if h.As != "" {
			rec = map[string]any{"type": h.As, "conversation": conv, "data": el}
		}
		if b, err := json.Marshal(rec); err == nil {
			events = append(events, b)
		}
	}
	return events, nil
}

func heldOf(h *heldRecords) []harness.Record {
	if h == nil {
		return nil
	}
	return h.recs
}

func (e *Engine) conversationPath(id string) string {
	if e.cfg.TogoHome == "" {
		return ""
	}
	if _, err := session.ValidateID(id); err != nil {
		return ""
	}
	return filepath.Join(e.cfg.TogoHome, "run", "harness", id+".conversation")
}

// saveConversation remembers the conversation a session shows across a
// helper restart. It is removed with the session (Forget) and when the
// agent is restarted, which may open another conversation.
func (e *Engine) saveConversation(id, conv string) {
	path := e.conversationPath(id)
	if path == "" || !conversationID.MatchString(conv) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, []byte(conv), 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func (e *Engine) loadConversation(id string) string {
	path := e.conversationPath(id)
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil || !conversationID.Match(b) {
		return ""
	}
	return string(b)
}

func (e *Engine) forgetConversation(id string) {
	if path := e.conversationPath(id); path != "" {
		_ = os.Remove(path)
	}
}
