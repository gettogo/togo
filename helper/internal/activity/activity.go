// Package activity keeps a short, bounded history of how much output each
// session produced, for the roster's activity bars.
package activity

import (
	"sync"
	"time"
)

const (
	// Buckets is how many buckets History returns.
	Buckets = 16
	// BucketSpan is the time one bucket covers.
	BucketSpan = 30 * time.Second
)

// secondsPerBucket is the most distinct seconds a bucket can count.
const secondsPerBucket = int64(BucketSpan / time.Second)

type series struct {
	// counts[i] holds the seconds with output in bucket index starts+i,
	// as a ring over the last Buckets bucket indexes.
	counts [Buckets]int16
	// bucket is the newest bucket index written; lastSec the last unix
	// second counted, so several signals in one second count once.
	bucket  int64
	lastSec int64
	// shown is true while the last history handed out had output in it.
	shown bool
}

// MaxSessions bounds how many sessions are tracked; the least recently
// active one makes room for a new one.
const MaxSessions = 512

// Tracker records output per session. Memory is a fixed few dozen bytes per
// session; recording is a map lookup and an increment.
type Tracker struct {
	mu sync.Mutex
	by map[string]*series
}

// New returns an empty Tracker.
func New() *Tracker { return &Tracker{by: map[string]*series{}} }

func bucketOf(t time.Time) int64 { return t.Unix() / secondsPerBucket }

// advance moves s so its newest bucket is b, clearing the buckets passed.
func (s *series) advance(b int64) {
	if b <= s.bucket {
		return
	}
	if b-s.bucket >= Buckets {
		s.counts = [Buckets]int16{}
	} else {
		for i := s.bucket + 1; i <= b; i++ {
			s.counts[i%Buckets] = 0
		}
	}
	s.bucket = b
}

// Record notes that the session produced output at the given time.
func (t *Tracker) Record(id string, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.by[id]
	b := bucketOf(at)
	if s == nil {
		if len(t.by) >= MaxSessions {
			t.evictLocked()
		}
		s = &series{bucket: b}
		t.by[id] = s
	}
	if b < s.bucket {
		return
	}
	s.advance(b)
	if sec := at.Unix(); sec != s.lastSec {
		s.lastSec = sec
		s.counts[b%Buckets]++
	}
}

// History returns Buckets values in 0...1, oldest first, ending with the
// bucket that contains now. Each value is the share of that bucket's seconds
// in which the session produced output. It returns nil for a session with no
// recorded output, meaning unknown.
func (t *Tracker) History(id string, now time.Time) []float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.by[id]
	if s == nil {
		return nil
	}
	s.advance(bucketOf(now))
	out := make([]float64, Buckets)
	cur := s.bucket
	for i := 0; i < Buckets; i++ {
		b := cur - int64(Buckets-1-i)
		if b < 0 {
			continue
		}
		v := float64(s.counts[b%Buckets]) / float64(secondsPerBucket)
		out[i] = min(v, 1)
	}
	return out
}

// evictLocked drops the session whose newest output is oldest.
func (t *Tracker) evictLocked() {
	var oldest string
	var at int64
	for id, s := range t.by {
		if oldest == "" || s.lastSec < at {
			oldest, at = id, s.lastSec
		}
	}
	delete(t.by, oldest)
}

// Rolling returns the sessions whose history should be sent again at now:
// those with output inside the window, so their bars move as buckets pass,
// and, once, those whose last output has just left it, so their bars clear.
func (t *Tracker) Rolling(now time.Time) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for id, s := range t.by {
		s.advance(bucketOf(now))
		live := false
		for _, c := range s.counts {
			if c > 0 {
				live = true
				break
			}
		}
		if live || s.shown {
			out = append(out, id)
		}
		s.shown = live
	}
	return out
}

// Forget drops a session's history.
func (t *Tracker) Forget(id string) {
	t.mu.Lock()
	delete(t.by, id)
	t.mu.Unlock()
}
