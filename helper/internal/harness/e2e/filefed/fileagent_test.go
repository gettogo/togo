package filefed

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// fileAgent is an agent whose chat comes from files or a database its own
// process writes, not from hooks: the generic scenarios below write its
// records with the profile while the engine follows them, and check the
// engine-level rules after each step.
type fileAgent struct {
	name    string
	profile func() sessiongen.Profile
	// cont is the Prompt that continues a turn left open (Hook.Pending).
	cont string
	// limit bounds each wait, when it is not fileLimit.
	limit time.Duration
	// opts adds to the rig's options.
	opts func(o *enginetest.Options)
}

func (a fileAgent) start(t *testing.T, e support.Env, s sessiongen.Script) (*sessiongen.Layout, *enginetest.Rig) {
	t.Helper()
	l, err := sessiongen.Write(e.Home, a.profile(), sessiongen.Script{Seed: s.Seed, Start: e.Start, Cwd: e.Cwd})
	if err != nil {
		t.Fatal(err)
	}
	o := enginetest.Options{Agent: a.name, NoRedact: true}
	if a.opts != nil {
		a.opts(&o)
	}
	r := e.RigWith(t, o)
	// The feeds attach to the files that exist when they start; the agent
	// writes on after that.
	time.Sleep(1200 * time.Millisecond)
	if len(s.Turns) > 0 {
		a.add(t, l, s.Turns...)
	}
	return l, r
}

func (a fileAgent) add(t *testing.T, l *sessiongen.Layout, turns ...sessiongen.Turn) {
	t.Helper()
	if err := l.Append(turns...); err != nil {
		t.Fatal(err)
	}
}

// attach writes a first turn left open, waits for the chat to show it,
// then ends it: from here the engine follows the agent's file. Start's
// pause does not prove that on a busy machine, and finished turns written
// before the feed found its file may never show: Vibe starts an empty
// journal segment after each turn, and a feed that begins between turns
// follows that one (TestKnownFault_Vibe_RestoreBetweenTurnsShowsNothing).
// A turn still open is read whenever the feed begins.
func (a fileAgent) attach(t *testing.T, r *enginetest.Rig, l *sessiongen.Layout) {
	t.Helper()
	a.add(t, l, open(sessiongen.Turn{Say: 30, Tools: 1, Output: 50}))
	a.seen(t, r, "attached, mid turn", l, true)
	a.add(t, l, sessiongen.Turn{Prompt: a.cont, Say: 30})
	a.seen(t, r, "attached", l, false)
}

// open marks a turn as left open at its last tool call.
func open(t sessiongen.Turn) sessiongen.Turn {
	h := sessiongen.HookTurn{}
	if t.Hook != nil {
		h = *t.Hook
	}
	h.Pending = true
	t.Hook = &h
	return t
}

// with sets the turn's feature fields.
func with(t sessiongen.Turn, f func(h *sessiongen.HookTurn)) sessiongen.Turn {
	h := sessiongen.HookTurn{}
	if t.Hook != nil {
		h = *t.Hook
	}
	f(&h)
	t.Hook = &h
	return t
}

// reached waits for the newest item the generator wrote, and for every
// turn it shows to have ended or be the one left open.
func reached(l *sessiongen.Layout, openTurn bool) func(enginetest.State) bool {
	return func(st enginetest.State) bool {
		if !enginetest.Has(l.Truth.Last)(st) {
			return false
		}
		if openTurn {
			return true
		}
		for _, it := range latestItems(st) {
			if it.Turn != nil && support.Running(it.Turn.State) {
				return false
			}
		}
		return true
	}
}

func latestItems(st enginetest.State) map[string]harness.Item {
	m := map[string]harness.Item{}
	for _, it := range st.Items {
		if old, ok := m[it.ID]; !ok || it.Seq > old.Seq {
			m[it.ID] = it
		}
	}
	return m
}

// seen settles on what the generator wrote; midTurn is true while a turn
// is left open, where the rules that need a finished turn do not apply.
func (a fileAgent) seen(t *testing.T, r *enginetest.Rig, where string, l *sessiongen.Layout, midTurn bool) enginetest.State {
	t.Helper()
	ok := reached(l, midTurn)
	limit := a.limit
	if limit == 0 {
		limit = support.FileLimit
	}
	limit = max(limit, enginetest.MinSettleLimit)
	deadline := time.Now().Add(limit)
	st := r.SettleUntil(limit, nil)
	for !ok(st) && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		st = r.SettleUntil(limit, nil)
	}
	if !ok(st) {
		t.Errorf("%s: the chat never reached the generator's last item %q (%d items, summary %+v)", where, l.Truth.Last, len(st.Items), st.Mission.Summary)
		for _, it := range st.Items[max(len(st.Items)-8, 0):] {
			t.Logf("  %d %s %s %s", it.Seq, it.Kind, it.ID, view(it))
		}
		t.FailNow()
	}
	vs := enginetest.Check(st, enginetest.Facts{Truth: l.Truth, Over: !midTurn})
	// A turn the user interrupted has no reply to wait for.
	kept := vs[:0]
	for _, v := range vs {
		if v.Rule != "prompt-unanswered" || !strings.Contains(v.Detail, ", interrupted)") {
			kept = append(kept, v)
		}
	}
	support.Fail(t, where, kept, l)
	return st
}

// view is an item's state, for a failure message.
func view(it harness.Item) string {
	switch {
	case it.Turn != nil:
		return it.Turn.State
	case it.Tool != nil:
		return it.Tool.State
	}
	return ""
}

// restartsAtSeededPoints writes a session in steps. At each seeded point
// the helper is down while the agent works up to a turn left open mid-way;
// the new engine restores it, restarts once more, and must show the same
// chat both times; then the turn ends under the running engine and the
// session goes on. (Several finished turns are never written under a live
// engine: Vibe keeps two journal segments, and the helper looks for new
// ones every two seconds.)
func (a fileAgent) restartsAtSeededPoints(t *testing.T, seed int64, nTurns, points int) int {
	e := support.NewEnv(t, time.Hour)
	l, r := a.start(t, e, sessiongen.Script{Seed: seed})
	cut := support.Cuts(seed, points, 1, nTurns-1)
	done, restarts := 0, 0
	mk := func(i int) sessiongen.Turn { return sessiongen.Turn{Say: 80, Tools: 1 + i%3, Output: 300} }
	for _, c := range cut {
		r.Stop()
		for ; done < c; done++ {
			a.add(t, l, mk(done))
		}
		a.add(t, l, open(mk(done)))
		r.Start()
		first := a.seen(t, r, "restored at "+strconv.Itoa(c), l, true)
		r.Restart()
		second := a.seen(t, r, "restored again at "+strconv.Itoa(c), l, true)
		support.Fail(t, "restart "+strconv.Itoa(c), enginetest.Restarted(first, second), l)
		a.add(t, l, sessiongen.Turn{Prompt: a.cont, Say: 60, Tools: 1, Output: 100})
		done++
		restarts++
		a.seen(t, r, "after the open turn ended "+strconv.Itoa(c), l, false)
	}
	r.Stop()
	for ; done < nTurns; done++ {
		a.add(t, l, mk(done))
	}
	a.add(t, l, open(mk(done)))
	r.Start()
	st := a.seen(t, r, "end", l, true)
	r.Restart()
	again := a.seen(t, r, "end after restart", l, true)
	support.Fail(t, "end restart", enginetest.Restarted(st, again), l)
	a.add(t, l, sessiongen.Turn{Prompt: a.cont, Say: 60})
	a.seen(t, r, "last turn ended", l, false)
	return restarts
}

// rereadsAgree: two restores of the same files agree.
func rereadsAgree(t *testing.T, r *enginetest.Rig, l *sessiongen.Layout, last func(enginetest.State) bool) {
	t.Helper()
	r.Restart()
	a := r.SettleUntil(support.FileLimit, last)
	r.Restart()
	b := r.SettleUntil(support.FileLimit, last)
	support.Fail(t, "second restore", enginetest.Reread(a, b), l)
}

// reconnect: a client's connection drops while the agent works and comes
// back with the cursor it had; it gets exactly the items that arrived
// meanwhile, each once, under the same epoch.
func (a fileAgent) reconnect(t *testing.T) {
	e := support.NewEnv(t, time.Hour)
	l, r := a.start(t, e, sessiongen.Script{Seed: 61})
	a.attach(t, r, l)
	for i := 0; i < 3; i++ {
		a.add(t, l, sessiongen.Turn{Say: 50, Tools: 2, Output: 100})
	}
	a.seen(t, r, "first half", l, false)
	ctx := context.Background()
	sub := &support.QuietSub{DoneCh: make(chan struct{})}
	first, err := r.Engine.Open(ctx, sub, r.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Engine.Close(ctx, sub, r.ID)
	close(sub.DoneCh) // the connection drops
	for i := 0; i < 4; i++ {
		a.add(t, l, sessiongen.Turn{Say: 50, Tools: 1, Output: 100})
	}
	st := a.seen(t, r, "second half", l, false)
	sub2 := &support.QuietSub{DoneCh: make(chan struct{})}
	back, err := r.Engine.Open(ctx, sub2, r.ID, first.Cursor, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer close(sub2.DoneCh)
	if back.Epoch != first.Epoch {
		t.Errorf("the epoch changed from %q to %q while the client was away", first.Epoch, back.Epoch)
	}
	want := map[string]int64{}
	for _, it := range st.Items {
		if it.Seq > first.Cursor {
			want[it.ID] = it.Seq
		}
	}
	got := map[string]int64{}
	for _, it := range back.Items {
		if _, dup := got[it.ID]; dup {
			t.Errorf("%s comes twice after the reconnect", it.ID)
		}
		got[it.ID] = it.Seq
	}
	if len(got) != len(want) || len(want) == 0 {
		t.Errorf("the reconnect returned %d items, %d arrived meanwhile", len(got), len(want))
	}
	for id, seq := range want {
		if got[id] != seq {
			t.Errorf("%s: seq %d after the reconnect, %d in the log", id, got[id], seq)
		}
	}
}

// removed: the hub reports the session gone while the agent works on.
func (a fileAgent) removed(t *testing.T) {
	e := support.NewEnv(t, time.Hour)
	l, r := a.start(t, e, sessiongen.Script{Seed: 62})
	a.add(t, l, sessiongen.Turn{Say: 40, Tools: 2, Output: 100}, open(sessiongen.Turn{Say: 40, Tools: 2, Output: 100}))
	a.seen(t, r, "before", l, true)
	r.Engine.Forget(r.ID)
	st := r.Read()
	if len(st.Items) != 0 || st.Mission.Summary.Working || st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("after the removal: %d items, summary %+v", len(st.Items), st.Mission.Summary)
	}
	for _, sec := range st.Mission.Sections {
		if len(sec.Rows) != 0 {
			t.Errorf("section %s still has %d rows", sec.Kind, len(sec.Rows))
		}
	}
}

// hammer reads the session from many goroutines while the agent writes.
func (a fileAgent) hammer(t *testing.T) {
	e := support.NewEnv(t, time.Hour)
	l, r := a.start(t, e, sessiongen.Script{Seed: 63})
	a.attach(t, r, l)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ctx := context.Background()
				sub := &support.QuietSub{DoneCh: make(chan struct{})}
				res, err := r.Engine.Open(ctx, sub, r.ID, 0, 20+g)
				if err != nil {
					t.Errorf("open: %v", err)
					return
				}
				if len(res.Items) > 0 {
					if _, err := r.Engine.History(ctx, r.ID, res.Items[0].Seq, 10); err != nil {
						t.Errorf("history: %v", err)
						return
					}
				}
				_, _ = r.Engine.Describe(ctx, r.ID)
				r.Engine.Summary(r.ID)
				_ = r.Engine.Close(ctx, sub, r.ID)
				close(sub.DoneCh)
			}
		}(g)
	}
	for i := 0; i < 25; i++ {
		a.add(t, l, sessiongen.Turn{Say: 50, Tools: 2, Output: 200})
		time.Sleep(10 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
	st := a.seen(t, r, "after the storm", l, false)
	enginetest.Fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(7)))
}
