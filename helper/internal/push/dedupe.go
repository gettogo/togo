package push

import "sync"

// maxSeenRequests caps how many request ids the Sender remembers. An
// approval lives at most a few TTLs, so the oldest ids are long settled.
const maxSeenRequests = 512

const (
	seenPushed = iota + 1 // a needs_you for the request went out
	seenClosed            // the request closed: it never rings again
)

// requestSeen remembers which requests already rang or closed, so a
// needs_you push goes out at most once per request id and never after the
// request was answered, withdrawn or made moot.
type requestSeen struct {
	mu    sync.Mutex
	m     map[string]int
	order []string
}

// blocks reports whether a needs_you for id must not go out.
func (d *requestSeen) blocks(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.m[id] != 0
}

// mark records id as pushed or closed. Closed is final.
func (d *requestSeen) mark(id string, state int) {
	if id == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m == nil {
		d.m = map[string]int{}
	}
	prev, known := d.m[id]
	if prev == seenClosed {
		return
	}
	d.m[id] = state
	if known {
		return
	}
	d.order = append(d.order, id)
	if len(d.order) > maxSeenRequests {
		drop := len(d.order) - maxSeenRequests
		for _, old := range d.order[:drop] {
			delete(d.m, old)
		}
		d.order = append([]string(nil), d.order[drop:]...)
	}
}
