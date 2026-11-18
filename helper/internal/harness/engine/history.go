package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
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
	events, partial, err := e.readHistory(ctx, s, s.ad.Feeds[feed].History, conv)
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.held
	s.held = nil
	if s.ctx.Err() != nil {
		return
	}
	moved := s.convGen != gen || (s.conv != "" && s.conv != conv)
	// say: older messages exist that the chat does not show.
	say := false
	if err != nil && !moved {
		e.cfg.Logf("harness: %s: the conversation's earlier messages could not be read: %v", s.ad.Adapter.Adapter, err)
		// Nothing of the past could be read. The chat goes on with what
		// happens from now, and says so: recent messages alone must not
		// pass for the whole conversation. The read is tried again on the
		// next record, a bounded number of times for one conversation.
		say = true
		if s.histFailed != conv {
			s.histFailed, s.histFails = conv, 0
		}
		s.histFails++
		if s.hydrated == conv && s.histFails < historyRetries {
			s.hydrated = ""
		}
	}
	if err == nil && !moved {
		if len(events) > 0 {
			floor := e.log.RetentionFloor(s.id)
			s.mappers[mapper] = e.mapperFor(mapper, s.env)
			s.restoreLocked(s.ctx, mapper, harness.Record{Source: feed, At: e.cfg.Now(), Replay: events})
			// Not all was read (a bound, a later page that failed), or the
			// replay itself was more than a session keeps and the oldest
			// were dropped.
			say = partial || e.log.RetentionFloor(s.id) > floor
		} else {
			say = partial // a first page with nothing in it that still names an older one
		}
	}
	// Said in fixed words, as an ordinary item after the replay: it gets
	// the next number like any other, so a chat that reconnects by number
	// receives it, and the replay cannot push it out. When a later read
	// does bring the whole past, the same item says that instead.
	if !moved {
		switch {
		case say:
			s.histNoticed = conv
			s.publishLocked([]harness.Item{{ID: "history-partial-" + conv, Kind: harness.KindNotice, At: e.cfg.Now(),
				Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeFallback, Text: olderInTerminal}}}, nil)
		case err == nil && s.histNoticed == conv:
			s.histNoticed = ""
			s.publishLocked([]harness.Item{{ID: "history-partial-" + conv, Kind: harness.KindNotice, At: e.cfg.Now(),
				Notice: &harness.NoticeBody{Level: "info", Code: harness.NoticeFallback, Text: olderLoaded}}}, nil)
		}
	}
	// Held records are mapped as they would have been, after the past.
	for _, rec := range heldOf(held) {
		s.recordLocked(mapper, rec)
	}
}

// maxHistory bounds the source records one history read replays. A record
// may map to no item or to several: this is not the item bound (MaxItems).
const maxHistory = 2000

// Bounds on reading history in pages: the pages (a route's own default, and
// the most an adapter may ask for), the cursor's length, and the bytes of
// all answers kept together.
const (
	historyPages     = 20
	historyPagesMax  = 50
	historyCursorMax = 2048
	historyBytesMax  = 8 << 20
)

// historyRetries is how many times one conversation's past is read again
// after a read that failed outright.
const historyRetries = 3

// olderLoaded replaces the line below once a later read brought the past.
const olderLoaded = "Earlier messages loaded."

// olderInTerminal is what the chat says when a conversation has messages
// older than the ones it shows.
const olderInTerminal = "Earlier messages are not shown here. Open the terminal to see them."

// readHistory asks the harness for the conversation's past and returns it
// as the feed mapper's records, oldest first. Where the route pages, the
// newest page is read first and older ones are put in front of it, until
// there is no older page or a bound is met. partial reports that older
// records exist that were not read; what was read is still returned. Only
// a first page that cannot be read is an error.
func (e *Engine) readHistory(ctx context.Context, s *sess, h *spec.History, conv string) (events []json.RawMessage, partial bool, err error) {
	r, ok := harness.LookupRunner(spec.KindHTTP)
	if !ok {
		return nil, false, errors.New("no http runner")
	}
	first := strings.ReplaceAll(h.Path, "{{.conversation}}", conv)
	fetch := func(path string) (any, int, error) {
		raw, err := json.Marshal(spec.HTTP{Method: "GET", Path: path})
		if err != nil {
			return nil, 0, err
		}
		out, err := r.Fetch(ctx, harness.SourceSpec{Name: "history", Kind: spec.KindHTTP, Raw: raw}, s.env)
		if err != nil {
			return nil, 0, err
		}
		v, err := decode(out)
		if err != nil {
			return nil, 0, errors.New("history is not JSON")
		}
		return v, len(out), nil
	}
	paged := h.Page != nil && h.Page.Cursor != "" && h.Page.Param != ""
	listOf := func(v any) ([]any, error) {
		if h.Items != "" {
			var err error
			if v, err = JQFirst(h.Items, v, nil); err != nil {
				return nil, err
			}
		}
		list, ok := v.([]any)
		// A paged route's answer must be a list: anything else is not a
		// page with nothing in it, and is not taken for one.
		if !ok && paged {
			return nil, errors.New("history page is not a list")
		}
		return list, nil
	}
	answer, size, err := fetch(first)
	if err != nil {
		return nil, false, err
	}
	list, err := listOf(answer)
	if err != nil {
		return nil, false, err
	}
	if p := h.Page; paged {
		pages := p.MaxPages
		if pages <= 0 {
			pages = historyPages
		}
		pages = min(pages, historyPagesMax)
		seen := map[string]bool{}
		for page := 1; ; page++ {
			// The start is reached only when the answer says so: a cursor
			// that is absent, null or empty. A cursor that cannot be read,
			// or is not text, says nothing about the start.
			c, cerr := JQFirst(p.Cursor, answer, nil)
			cursor, isText := c.(string)
			if cerr != nil || (c != nil && !isText) {
				partial = true
				break
			}
			if cursor == "" {
				break // the start of the conversation was reached
			}
			// From here on something older exists. Any stop is partial.
			partial = true
			if page >= pages || len(list) >= maxHistory || size >= historyBytesMax ||
				len(cursor) > historyCursorMax || seen[cursor] || ctx.Err() != nil {
				break
			}
			seen[cursor] = true
			next, ok := cursorPath(first, p, cursor)
			if !ok {
				break
			}
			older, n, err := fetch(next)
			if err != nil || size+n > historyBytesMax {
				break
			}
			olderList, err := listOf(older)
			if err != nil {
				break
			}
			// An older page goes in front: each page is already oldest
			// first within itself.
			list = append(olderList, list...)
			answer, size, partial = older, size+n, false
		}
	}
	// The newest records are the ones kept.
	if len(list) > maxHistory {
		list, partial = list[len(list)-maxHistory:], true
	}
	events = make([]json.RawMessage, 0, len(list))
	seenRecord := map[string]bool{}
	for _, el := range list {
		var rec any = el
		if h.As != "" {
			rec = map[string]any{"type": h.As, "conversation": conv, "data": el}
		}
		b, err := json.Marshal(rec)
		if err != nil {
			continue
		}
		// Pages that overlap list a record twice: it is replayed once.
		if key := string(b); seenRecord[key] {
			continue
		} else {
			seenRecord[key] = true
		}
		events = append(events, b)
	}
	return events, partial, nil
}

// cursorPath is the first request's path with the cursor added and the
// parameters that may not accompany one removed. The cursor is passed as it
// came, encoded as a query value.
func cursorPath(first string, p *spec.HistoryPage, cursor string) (string, bool) {
	u, err := url.Parse(first)
	if err != nil {
		return "", false
	}
	q := u.Query()
	for _, name := range p.Omit {
		q.Del(name)
	}
	q.Set(p.Param, cursor)
	u.RawQuery = q.Encode()
	return u.String(), true
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
