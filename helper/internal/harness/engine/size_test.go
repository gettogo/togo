package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func bigItems(n, size int) []harness.Item {
	out := make([]harness.Item, n)
	for i := range out {
		out[i] = harness.Item{ID: "i" + strings.Repeat("x", i%3) + string(rune('a'+i%26)), Kind: harness.KindAssistant, Seq: int64(i + 1),
			Assistant: &harness.TextBody{Text: strings.Repeat("y", size)}}
	}
	return out
}

func TestChunkItemsBySize(t *testing.T) {
	items := bigItems(40, 50<<10)
	chunks := chunkItems(items, batchBytes)
	n := 0
	for _, c := range chunks {
		size := 0
		for _, it := range c {
			size += itemSize(it)
		}
		if size > batchBytes && len(c) > 1 {
			t.Fatalf("chunk of %d bytes", size)
		}
		for _, it := range c {
			if it.Seq != int64(n+1) {
				t.Fatalf("order broken at %d", n)
			}
			n++
		}
	}
	if n != len(items) || len(chunks) < 8 {
		t.Fatalf("%d items in %d chunks", n, len(chunks))
	}
}

func TestNewestFittingKeepsTail(t *testing.T) {
	items := bigItems(30, 50<<10)
	fit, cut := newestFitting(items, pageBytes)
	if !cut || len(fit) == 0 || fit[len(fit)-1].Seq != 30 || len(fit) >= 30 {
		t.Fatalf("kept %d, cut %v", len(fit), cut)
	}
	if fit, cut := newestFitting(items[:3], pageBytes); cut || len(fit) != 3 {
		t.Fatal("a small page was cut")
	}
}

type failSub struct {
	mu   sync.Mutex
	got  []harness.Notification
	done chan struct{}
}

func (f *failSub) Notify(n harness.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n.Kind == harness.NoteItems {
		return errors.New("frame too large")
	}
	f.got = append(f.got, n)
	return nil
}

func (f *failSub) Done() <-chan struct{} { return f.done }

// A notification that cannot be sent is followed by a reset, so the phone
// reopens instead of freezing.
func TestFailedNotifySendsReset(t *testing.T) {
	sub := &failSub{done: make(chan struct{})}
	q := newSubQueue(sub, "s_x", func() string { return "e1" }, time.Now, nil)
	q.release()
	q.push(harness.Notification{Kind: harness.NoteItems})
	waitFor(t, func() bool { sub.mu.Lock(); defer sub.mu.Unlock(); return len(sub.got) == 1 })
	if sub.got[0].Kind != harness.NoteReset {
		t.Fatalf("got %+v", sub.got)
	}
}

// A batch reads the pane once for all its new items, and replayed items
// (old timestamps) get no anchor at all.
func TestAnchorsOncePerBatchAndNotForReplay(t *testing.T) {
	const id = "s_anchorb01"
	e := newSeq(t, id, &seqPane{mode: "a"}, nil)
	m := &fakeMarker{}
	e.cfg.Marker = m
	s, _ := e.served(context.Background(), id)
	now := e.cfg.Now()
	s.mu.Lock()
	s.publishLocked([]harness.Item{
		{ID: "a1", Kind: harness.KindAssistant, At: now, Assistant: &harness.TextBody{Text: "x"}},
		{ID: "a2", Kind: harness.KindAssistant, At: now, Assistant: &harness.TextBody{Text: "y"}},
		{ID: "old", Kind: harness.KindAssistant, At: now.Add(-time.Hour), Assistant: &harness.TextBody{Text: "z"}},
	}, nil)
	s.mu.Unlock()
	if m.n != 1 {
		t.Fatalf("pane read %d times", m.n)
	}
	a1, _ := e.log.Get(id, "a1")
	a2, _ := e.log.Get(id, "a2")
	old, _ := e.log.Get(id, "old")
	if a1.Anchor == nil || a2.Anchor == nil || *a1.Anchor != *a2.Anchor || old.Anchor != nil {
		t.Fatalf("anchors %+v %+v %+v", a1.Anchor, a2.Anchor, old.Anchor)
	}
}
