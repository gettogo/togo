package enginetest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// PageSize is the number of main-conversation items a page asks for, as
// the app does.
const PageSize = 50

// State is what a client can see of the session: every item, read page
// by page to the start, the pages as they came, and the mission.
type State struct {
	Items   []harness.Item
	Pages   [][]harness.Item
	Limit   int
	More    []bool // per page, whether it said older items exist
	Mission harness.Mission
	Epoch   string
	// Discarded is the Seq the log dropped items through (0: none).
	Discarded int64
}

// Read opens the session with a fresh subscriber and reads every history
// page, newest first, to the start of the log.
func (r *Rig) Read() State {
	r.T.Helper()
	return r.ReadPages(PageSize)
}

// ReadPages is Read with a chosen page size.
func (r *Rig) ReadPages(limit int) State {
	r.T.Helper()
	ctx := context.Background()
	sub := newSub()
	defer close(sub.done)
	res, err := r.Engine.Open(ctx, sub, r.ID, 0, limit)
	if err != nil {
		r.T.Fatalf("open: %v", err)
	}
	_ = r.Engine.Close(ctx, sub, r.ID)
	st := State{Limit: limit, Mission: res.Mission, Epoch: res.Epoch}
	if res.DiscardedThrough != nil {
		st.Discarded = *res.DiscardedThrough
	}
	st.Pages = append(st.Pages, res.Items)
	st.More = append(st.More, res.More)
	items := res.Items
	more := res.More
	for i := 0; more && len(items) > 0 && i < 10000; i++ {
		p, err := r.Engine.History(ctx, r.ID, items[0].Seq, limit)
		if err != nil {
			r.T.Fatalf("history: %v", err)
		}
		if len(p.Items) == 0 {
			break
		}
		st.Pages = append([][]harness.Item{p.Items}, st.Pages...)
		st.More = append([]bool{p.More}, st.More...)
		items = append(append([]harness.Item(nil), p.Items...), items...)
		more = p.More
	}
	st.Items = items
	return st
}

// Settle waits until the session's log and mission stop changing: the
// same newest Seq and mission revision on reads quiet apart. The feeds
// poll files every 20 ms and look for new files every 2 s, so a file
// written after the engine started needs up to that long. It fails the
// test after limit.
func (r *Rig) Settle(limit time.Duration) State {
	r.T.Helper()
	return r.SettleUntil(limit, nil)
}

// SettleUntil waits until ok holds (nil: always) and the state is quiet.
func (r *Rig) SettleUntil(limit time.Duration, ok func(State) bool) State {
	r.T.Helper()
	deadline := time.Now().Add(limit)
	var last string
	quiet := 0
	for {
		st := r.ReadPages(1 << 20)
		key := fingerprint(st)
		if key == last && (ok == nil || ok(st)) {
			quiet++
			// Quiet for longer than a file poll and a summary debounce.
			if quiet >= 6 {
				return r.Read()
			}
		} else {
			quiet = 0
		}
		last = key
		if time.Now().After(deadline) {
			if ok != nil && !ok(st) {
				r.T.Fatalf("the session never reached the state the test waits for: %d items, mission rev %d", len(st.Items), st.Mission.Rev)
			}
			return r.Read()
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Has is true once the log holds an item with this id, such as the
// generator's Truth.Last.
func Has(id string) func(State) bool {
	return func(st State) bool {
		for _, it := range st.Items {
			if it.ID == id {
				return true
			}
		}
		return false
	}
}

func fingerprint(st State) string {
	var top int64
	for _, it := range st.Items {
		top = max(top, it.Seq)
	}
	b, _ := json.Marshal(struct {
		Top int64
		N   int
		M   harness.Mission
	}{top, len(st.Items), st.Mission})
	return string(b)
}
