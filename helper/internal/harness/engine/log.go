package engine

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/gettogo/togo/helper/internal/harness"
)

// MaxItems is how many items a session keeps.
const MaxItems = 2000

// Log implements harness.Log: per session, a ring of items in Seq order
// where an item sent again replaces the old copy and moves to the end.
type Log struct {
	// NewEpoch names a new log; nil means random hex.
	NewEpoch func() string
	// Max bounds each session's ring; zero means MaxItems.
	Max int

	mu       sync.Mutex
	sessions map[string]*ring
}

type ring struct {
	epoch string
	seq   int64
	items []harness.Item // ascending Seq
}

var _ harness.Log = (*Log)(nil)

func randomEpoch() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (l *Log) get(session string) *ring {
	if l.sessions == nil {
		l.sessions = map[string]*ring{}
	}
	r, ok := l.sessions[session]
	if !ok {
		e := randomEpoch
		if l.NewEpoch != nil {
			e = l.NewEpoch
		}
		r = &ring{epoch: e()}
		l.sessions[session] = r
	}
	return r
}

// Epoch names the session's current log.
func (l *Log) Epoch(session string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.get(session).epoch
}

// Append stores items, replacing any with the same ID, and assigns each a
// new Seq.
func (l *Log) Append(session string, items []harness.Item) []harness.Item {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.get(session)
	out := make([]harness.Item, 0, len(items))
	for _, it := range items {
		for i := range r.items {
			if r.items[i].ID == it.ID {
				r.items = append(r.items[:i], r.items[i+1:]...)
				break
			}
		}
		r.seq++
		it.Seq = r.seq
		r.items = append(r.items, it)
		out = append(out, it)
	}
	limit := l.Max
	if limit <= 0 {
		limit = MaxItems
	}
	if n := len(r.items) - limit; n > 0 {
		r.items = append([]harness.Item(nil), r.items[n:]...)
	}
	return out
}

// Get returns the stored copy of an item.
func (l *Log) Get(session, id string) (harness.Item, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.get(session)
	for i := len(r.items) - 1; i >= 0; i-- {
		if r.items[i].ID == id {
			return r.items[i], true
		}
	}
	return harness.Item{}, false
}

// Since returns items with Seq > after, at most limit of the newest; the
// cursor is the newest Seq held, and more says older items exist before
// the first returned.
func (l *Log) Since(session string, after int64, limit int) ([]harness.Item, int64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.get(session)
	if after > r.seq {
		// A cursor this log never reached is from before a helper restart
		// (another epoch): start over rather than answer "nothing newer".
		after = 0
	}
	start := len(r.items)
	for i, it := range r.items {
		if it.Seq > after {
			start = i
			break
		}
	}
	sel := r.items[start:]
	if limit > 0 && len(sel) > limit {
		start += len(sel) - limit
		sel = sel[len(sel)-limit:]
	}
	return append([]harness.Item{}, sel...), r.seq, start > 0
}

// Before returns at most limit items with Seq < before, the newest of
// them, and whether older ones exist.
func (l *Log) Before(session string, before int64, limit int) ([]harness.Item, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.get(session)
	end := 0
	for end < len(r.items) && r.items[end].Seq < before {
		end++
	}
	start := 0
	if limit > 0 && end > limit {
		start = end - limit
	}
	return append([]harness.Item{}, r.items[start:end]...), start > 0
}

// Forget drops the session's log; the next use starts a new epoch.
func (l *Log) Forget(session string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.sessions, session)
}
