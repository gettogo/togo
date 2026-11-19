package hookstyle

import (
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/harness"
)

// mark is where a Mapper's running count and its reads of the harness's
// own log stand (harness.ReplayMarker).
type mark struct {
	N       int    `json:"n"`
	Session string `json:"s,omitempty"`
	From    int64  `json:"o,omitempty"`
}

var _ harness.ReplayMarker = (*Mapper)(nil)

// ReplayMark reports the count and the log offset.
func (m *Mapper) ReplayMark() json.RawMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, _ := json.Marshal(mark{N: m.n, Session: m.replySession, From: m.replyFrom})
	return b
}

// Replaying makes the next Map count from pre and read the log no further
// than post, without waiting: the log has long been written past what the
// record saw when it was first mapped.
func (m *Mapper) Replaying(pre, post json.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var p mark
	if pre != nil && json.Unmarshal(pre, &p) == nil {
		m.n, m.replySession, m.replyFrom = p.N, p.Session, p.From
	}
	m.upTo = nil
	var q mark
	if post != nil && json.Unmarshal(post, &q) == nil {
		m.upTo = &q
	}
}

// settle puts the mapper where a replayed record left it. Callers hold
// m.mu.
func (m *Mapper) settle() {
	if m.upTo == nil {
		return
	}
	m.n, m.replySession, m.replyFrom = m.upTo.N, m.upTo.Session, m.upTo.From
	m.upTo = nil
}

// readReplies reads the log after offset: up to where it ended when the
// record was first mapped, during a replay; to its end otherwise.
func (m *Mapper) readReplies(session string, offset int64) ([]Reply, int64) {
	if m.upTo == nil {
		return m.opts.Replies(m.env, session, offset, -1)
	}
	if session != m.upTo.Session {
		// The record read nothing of this log when it was first mapped.
		return nil, max(offset, 0)
	}
	return m.opts.Replies(m.env, session, offset, m.upTo.From)
}
