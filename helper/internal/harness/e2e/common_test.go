package e2e

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// starter writes the turns as a session of the last ten minutes and
// starts a rig over it.
type starter func(t *testing.T, turns ...sessiongen.Turn) (*sessiongen.Layout, *enginetest.Rig)

type dropSub struct{ done chan struct{} }

func (s dropSub) Notify(harness.Notification) error { return nil }
func (s dropSub) Done() <-chan struct{}             { return s.done }

// dropAndReturn: a phone that drops while the agent works and returns
// with the cursor it had gets exactly the items it missed, with no gap.
func dropAndReturn(t *testing.T, start starter) {
	l, r := start(t, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Say: 20})
	settleUntil(r, 2)
	a := dropSub{make(chan struct{})}
	first, err := r.Engine.Open(t.Context(), a, enginetest.SessionID, 0, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	r.Engine.Release(a, enginetest.SessionID)
	close(a.done) // the connection drops
	now(l)
	if err := l.Append(sessiongen.Turn{Say: 20, Tools: 2, Output: 20}, sessiongen.Turn{Say: 20}); err != nil {
		t.Fatal(err)
	}
	full := settleUntil(r, 4)
	b := dropSub{make(chan struct{})}
	defer close(b.done)
	back, err := r.Engine.Open(t.Context(), b, enginetest.SessionID, first.Cursor, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var want, got []string
	for _, it := range full.Items {
		if it.Seq > first.Cursor {
			want = append(want, it.ID)
		}
	}
	for _, it := range back.Items {
		got = append(got, it.ID)
	}
	if len(want) == 0 || strings.Join(want, ",") != strings.Join(got, ",") {
		t.Errorf("returning with cursor %d: got %v, want %v", first.Cursor, got, want)
	}
	if back.CoveredAfter == nil || *back.CoveredAfter != first.Cursor {
		t.Errorf("coveredAfter %v, want %d: the phone has no gap", back.CoveredAfter, first.Cursor)
	}
}

// removeAndServeAgain: a removed session is dropped by the engine; serving
// it again rebuilds the same conversation from the files under a new epoch.
func removeAndServeAgain(t *testing.T, start starter) {
	l, r := start(t, sessiongen.Turn{Say: 20, Tools: 1, Output: 20}, sessiongen.Turn{Say: 20})
	st := settleUntil(r, 2)
	r.Remove()
	again := settleUntil(r, 2)
	checkEdge(t, "after removal", again, l)
	if st.Epoch == again.Epoch {
		t.Error("the epoch is the same after the session was removed and served again")
	}
	fail(t, "removal", restarted(st, again))
}

// concurrentReaders: many clients reading at once while the agent writes
// get consistent answers: every read holds each id once.
func concurrentReaders(t *testing.T, start starter) {
	l, r := start(t, sessiongen.Turn{Say: 20, Tools: 2, Output: 20})
	settleUntil(r, 1)
	const readers = 8
	stop := make(chan struct{})
	errs := make(chan string, readers*64)
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := context.Background()
			for {
				select {
				case <-stop:
					return
				default:
				}
				sub := dropSub{make(chan struct{})}
				res, err := r.Engine.Open(ctx, sub, enginetest.SessionID, 0, 5+i)
				if err != nil {
					errs <- "open: " + err.Error()
					close(sub.done)
					return
				}
				seen := map[string]bool{}
				add := func(its []harness.Item) {
					for _, it := range its {
						if seen[it.ID] {
							errs <- "duplicate id " + it.ID
						}
						seen[it.ID] = true
					}
				}
				add(res.Items)
				for page, more := res.Items, res.More; more && len(page) > 0; {
					p, err := r.Engine.History(ctx, enginetest.SessionID, page[0].Seq, 5+i)
					if err != nil {
						errs <- "history: " + err.Error()
						break
					}
					add(p.Items)
					page, more = p.Items, p.More
				}
				_ = r.Engine.Close(ctx, sub, enginetest.SessionID)
				close(sub.done)
			}
		}(i)
	}
	for i := 0; i < 12; i++ {
		now(l)
		if err := l.Append(sessiongen.Turn{Say: 20, Tools: 2, Output: 20}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	st := settleUntil(r, 13)
	close(stop)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	checkEdge(t, "after the readers", st, l)
}
