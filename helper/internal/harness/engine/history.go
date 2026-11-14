package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
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
// past is read are held and mapped after it, leaving out the ones the past
// already holds (by event id), so no event is lost or applied twice.

// historyEnd is the event type that ends a history read.
const historyEnd = "log.synced"

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
	path := strings.ReplaceAll(s.ad.Feeds[feed].History.Path, "{{.conversation}}", conv)
	ctx, cancel := context.WithTimeout(s.ctx, historyTimeout)
	events, err := httpsrc.ReadLog(ctx, s.env, path, historyEnd)
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.held
	s.held = nil
	if s.ctx.Err() != nil {
		return
	}
	moved := s.convGen != gen || (s.conv != "" && s.conv != conv)
	if err != nil || moved {
		if err != nil {
			e.cfg.Logf("harness: %s: the conversation's earlier messages could not be read: %v", s.ad.Adapter.Adapter, err)
			if s.hydrated == conv {
				s.hydrated = "" // asked again on the next record
			}
		}
		// The chat moved on, or the past could not be read: the held
		// records are mapped as they would have been.
		for _, rec := range heldOf(held) {
			s.recordLocked(mapper, rec)
		}
		return
	}
	seen := map[string]bool{}
	for _, raw := range events {
		if id := eventID(raw); id != "" {
			seen[id] = true
		}
	}
	if len(events) > 0 {
		s.mappers[mapper] = e.mapperFor(mapper, s.env)
		s.restoreLocked(s.ctx, mapper, harness.Record{Source: feed, At: e.cfg.Now(), Replay: httpsrc.Newest(events, httpsrc.MaxLogEvents)})
	}
	for _, rec := range heldOf(held) {
		if id := eventID(rec.Data); id != "" && seen[id] {
			continue
		}
		s.recordLocked(mapper, rec)
	}
}

func heldOf(h *heldRecords) []harness.Record {
	if h == nil {
		return nil
	}
	return h.recs
}

func eventID(raw json.RawMessage) string {
	var head struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &head)
	return head.ID
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
