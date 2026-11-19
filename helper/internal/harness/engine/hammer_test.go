package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"runtime/pprof"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// The hammer calls every harness method from many goroutines at once,
// through the same method table the daemon serves, against a real engine
// with a scripted harness. The race detector reports unsynchronised access;
// the deadline reports calls that never return; the checks after the storm
// report a log or mission that no longer agrees with itself.

func TestHammerCoversEveryMethod(t *testing.T) {
	e := hammerEngine(t, hammerIDs(1)...)
	for name := range harness.Methods(e) {
		if hammerCalls[name] == nil {
			t.Errorf("%s has no entry in hammerCalls: add one so the hammer calls it", name)
		}
	}
	for name := range hammerCalls {
		if harness.Methods(e)[name] == nil {
			t.Errorf("hammerCalls names %s, which is not a method", name)
		}
	}
}

// storm is one run: callers call methods, writers deliver the agent's
// events, churners subscribe and unsubscribe, until each has done iters.
type storm struct {
	t      *testing.T
	e      *Engine
	ids    []string
	table  map[string]func(context.Context, json.RawMessage) (any, error)
	names  []string
	cur    sync.Map // goroutine name -> method being called
	fails  atomic.Int64
	forget bool // sessions are also removed and come back mid-storm
	calls  atomic.Int64
}

func (s *storm) failf(format string, a ...any) {
	if s.fails.Add(1) <= 10 {
		s.t.Errorf(format, a...)
	}
}

func newStorm(t *testing.T, ids []string) *storm {
	e := hammerEngine(t, ids...)
	s := &storm{t: t, e: e, ids: ids}
	m := harness.Methods(e)
	s.table = map[string]func(context.Context, json.RawMessage) (any, error){}
	for name, fn := range m {
		s.table[name] = fn
		s.names = append(s.names, name)
	}
	slices.Sort(s.names)
	return s
}

// call runs one method as the daemon does: params as JSON, the result
// encoded the way rpc encodes it. It checks what a response must satisfy.
func (s *storm) call(r *rand.Rand, who, name, id string) {
	raw, _ := json.Marshal(hammerCalls[name](r, id))
	s.cur.Store(who, name)
	defer s.cur.Delete(who)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := s.table[name](ctx, raw)
	s.calls.Add(1)
	if err != nil {
		// A session removed under a call may answer with a fallback.
		if mustRead[name] && !s.forget {
			s.failf("%s on %s: %v", name, id, err)
		}
		return
	}
	out, err := json.Marshal(res)
	if err != nil {
		s.failf("%s result does not encode: %v", name, err)
		return
	}
	switch name {
	case harness.MethodOpen, harness.MethodHistory:
		var p struct {
			Items []harness.Item `json:"items"`
		}
		if json.Unmarshal(out, &p) != nil {
			s.failf("%s result does not decode", name)
			return
		}
		if msg := checkItems(p.Items); msg != "" {
			s.failf("%s on %s: %s", name, id, msg)
		}
	}
}

// checkItems reports ids that repeat or seqs that do not strictly rise.
func checkItems(items []harness.Item) string {
	seen := map[string]bool{}
	var last int64
	for _, it := range items {
		if seen[it.ID] {
			return "item id " + it.ID + " appears twice"
		}
		seen[it.ID] = true
		if it.Seq <= last {
			return fmt.Sprintf("seq %d follows %d", it.Seq, last)
		}
		last = it.Seq
	}
	return ""
}

// write delivers one random agent event: items (new and re-sent), plan and
// needs rows, working, mode and model.
func (s *storm) write(r *rand.Rand, id string) {
	ev := map[string]any{}
	if r.Intn(3) > 0 {
		n := fmt.Sprintf("i%02d", r.Intn(hammerPool))
		it := harness.Item{ID: n, TurnID: n, Kind: harness.KindUser, User: &harness.UserBody{Text: "text " + n}}
		if r.Intn(2) == 0 {
			it = harness.Item{ID: n, TurnID: "t" + n, Kind: harness.KindAssistant, Assistant: &harness.TextBody{Text: "reply " + n, Partial: r.Intn(2) == 0}}
		}
		ev["items"] = []harness.Item{it}
	}
	if r.Intn(2) == 0 {
		row := &harness.Row{ID: fmt.Sprintf("r%d", r.Intn(8)), Title: "Step", State: []string{harness.RowPending, harness.RowActive, harness.RowDone}[r.Intn(3)]}
		sec := []string{harness.SectionPlan, harness.SectionPlan, harness.SectionNeeds}[r.Intn(3)]
		op := harness.Op{Op: harness.OpUpsert, Section: sec, Row: row}
		if r.Intn(5) == 0 {
			op = harness.Op{Op: harness.OpRemove, Section: sec, ID: row.ID}
		}
		ev["ops"] = []harness.Op{op}
	}
	if r.Intn(2) == 0 {
		ev["state"] = map[string]string{"working": fmt.Sprint(r.Intn(2) == 0), "model": []string{"x", "y"}[r.Intn(2)]}
	}
	deliver(id, ev)
}

// churn opens a subscriber, lets some notifications reach it, and drops it
// by Close, by done, or by neither.
func (s *storm) churn(r *rand.Rand, id string) {
	sub := &fakeSub{done: make(chan struct{})}
	ctx := context.Background()
	if _, err := s.e.Open(ctx, sub, id, 0, 10); err != nil {
		if !s.forget {
			s.failf("churn open: %v", err)
		}
		return
	}
	s.e.Release(sub, id)
	switch r.Intn(3) {
	case 0:
		_ = s.e.Close(ctx, sub, id)
	case 1:
		close(sub.done)
	}
	var last int64
	for _, n := range sub.notes() {
		for _, it := range n.Items {
			if it.Seq <= last && n.Kind == harness.NoteItems && len(n.Items) == 1 {
				s.failf("notification seq %d after %d for %s", it.Seq, last, id)
			}
			last = it.Seq
		}
	}
}

// run starts the goroutines and waits for them, or reports which calls are
// still blocked when the deadline passes.
func (s *storm) run(callers, iters int) {
	t := s.t
	var wg sync.WaitGroup
	start := make(chan struct{})
	spawn := func(who string, seed int64, f func(r *rand.Rand, i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					s.failf("%s panicked: %v", who, p)
				}
			}()
			r := rand.New(rand.NewSource(seed))
			<-start
			for i := 0; i < iters; i++ {
				f(r, i)
			}
		}()
	}
	pick := func(r *rand.Rand) string { return s.ids[r.Intn(len(s.ids))] }
	for c := 0; c < callers; c++ {
		who := fmt.Sprintf("caller%02d", c)
		spawn(who, int64(c)+1, func(r *rand.Rand, i int) {
			name := s.names[r.Intn(len(s.names))]
			// Mode changes wait for the harness to confirm; they are the
			// slow call, so they are the rare one.
			if name == harness.MethodAction && r.Intn(4) > 0 {
				name = harness.MethodComplete
			}
			s.call(r, who, name, pick(r))
		})
	}
	for w := 0; w < 4; w++ {
		spawn(fmt.Sprintf("writer%d", w), int64(100+w), func(r *rand.Rand, i int) { s.write(r, pick(r)) })
	}
	for w := 0; w < 3; w++ {
		who := fmt.Sprintf("churn%d", w)
		spawn(who, int64(200+w), func(r *rand.Rand, i int) {
			s.cur.Store(who, "subscribe")
			s.churn(r, pick(r))
			s.cur.Delete(who)
		})
	}
	if s.forget {
		spawn("forget", 300, func(r *rand.Rand, i int) {
			s.cur.Store("forget", "forget")
			defer s.cur.Delete("forget")
			if id := pick(r); r.Intn(2) == 0 {
				s.e.Forget(id)
			} else {
				ev := wire.Event{Kind: wire.EventSessionRemoved, SessionID: &id}
				s.e.Observe(ev)
			}
			time.Sleep(time.Duration(r.Intn(4)) * time.Millisecond)
		})
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	close(start)
	select {
	case <-done:
	case <-time.After(40 * time.Second):
		var blocked []string
		s.cur.Range(func(k, v any) bool { blocked = append(blocked, fmt.Sprintf("%v in %v", k, v)); return true })
		slices.Sort(blocked)
		var stacks bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&stacks, 1)
		out := stacks.String()
		if len(out) > 6000 {
			out = out[:6000]
		}
		t.Fatalf("deadlock: still blocked after 40s: %s\n%s", strings.Join(blocked, ", "), out)
	}
}

// settle waits until the agent's last event for the session is applied (one
// more sentinel item, delivered after the storm, has reached the log), then
// checks that what the session shows agrees with itself.
func (s *storm) settle() {
	t := s.t
	ctx := context.Background()
	for _, id := range s.ids {
		deliver(id, map[string]any{"items": []harness.Item{{ID: "zz-sentinel", TurnID: "zz", Kind: harness.KindUser, User: &harness.UserBody{Text: "end"}}}})
		waitFor(t, func() bool { _, ok := s.e.log.Get(id, "zz-sentinel"); return ok })
		// A queued prompt is released and re-sent when working ends, after
		// the sentinel; wait until the log stops moving.
		for prev, still := int64(-1), 0; still < 5; time.Sleep(30 * time.Millisecond) {
			it, _ := s.e.History(ctx, id, 1<<40, 1)
			c := int64(-2)
			if len(it.Items) > 0 {
				c = it.Items[0].Seq
			}
			if c == prev {
				still++
			} else {
				prev, still = c, 0
			}
		}
		res, err := s.e.Open(ctx, &fakeSub{done: make(chan struct{})}, id, 0, harness.MaxLimit)
		if err != nil {
			t.Errorf("%s: open after the storm: %v", id, err)
			continue
		}
		if msg := checkItems(res.Items); msg != "" {
			t.Errorf("%s: %s", id, msg)
		}
		if n := len(res.Items); n == 0 || res.Items[n-1].Seq != res.Cursor {
			t.Errorf("%s: cursor %d is not the newest item's seq (%d items)", id, res.Cursor, n)
		}
		if _, ok := s.e.log.Get(id, "zz-sentinel"); !ok {
			t.Errorf("%s: the last event is gone", id)
		}
		checkMission(t, id, res.Mission)
		if sum, ok := s.e.Summary(id); !ok || !sameJSON(sum, res.Mission.Summary) {
			t.Errorf("%s: Summary %+v differs from the mission's %+v", id, sum, res.Mission.Summary)
		}
		// Paging back from the newest item gives the same items, once each.
		var paged []harness.Item
		before := res.Cursor + 1
		for {
			p, err := s.e.History(ctx, id, before, 7)
			if err != nil {
				t.Errorf("%s: history: %v", id, err)
				break
			}
			if msg := checkItems(p.Items); msg != "" {
				t.Errorf("%s: history page: %s", id, msg)
			}
			paged = append(slices.Clone(p.Items), paged...)
			if !p.More || len(p.Items) == 0 {
				break
			}
			before = p.Items[0].Seq
		}
		if msg := checkItems(paged); msg != "" || len(paged) != len(res.Items) {
			t.Errorf("%s: paged history has %d items (%s), open has %d", id, len(paged), msg, len(res.Items))
		}
	}
}

// checkMission recomputes the summary from the rows it counts.
func checkMission(t *testing.T, id string, m harness.Mission) {
	t.Helper()
	var plan, needs *harness.Section
	for i := range m.Sections {
		switch m.Sections[i].Kind {
		case harness.SectionPlan:
			plan = &m.Sections[i]
		case harness.SectionNeeds:
			needs = &m.Sections[i]
		}
	}
	if needs != nil && m.Summary.NeedsYou != len(needs.Rows) {
		t.Errorf("%s: summary needs %d, rows %d", id, m.Summary.NeedsYou, len(needs.Rows))
	}
	switch {
	case plan == nil || len(plan.Rows) == 0:
		if m.Summary.Plan != nil {
			t.Errorf("%s: summary has a plan %+v, rows none", id, m.Summary.Plan)
		}
	case m.Summary.Plan == nil:
		t.Errorf("%s: summary has no plan, rows %d", id, len(plan.Rows))
	default:
		done := 0
		for _, r := range plan.Rows {
			if r.State == harness.RowDone || r.State == harness.RowSkipped {
				done++
			}
		}
		if m.Summary.Plan.Total != len(plan.Rows) || m.Summary.Plan.Done != done {
			t.Errorf("%s: summary plan %+v, rows %d of which %d done", id, m.Summary.Plan, len(plan.Rows), done)
		}
	}
}

func TestHammerOneSession(t *testing.T) {
	s := newStorm(t, hammerIDs(1))
	s.run(16, 120)
	s.settle()
	t.Logf("%d calls", s.calls.Load())
}

func TestHammerSeveralSessions(t *testing.T) {
	s := newStorm(t, hammerIDs(4))
	s.run(20, 120)
	s.settle()
	t.Logf("%d calls", s.calls.Load())
}

// Sessions removed and started again while calls, events and subscribers
// are in flight.
func TestHammerSessionsRemovedMidStorm(t *testing.T) {
	s := newStorm(t, hammerIDs(2))
	s.forget = true
	s.run(16, 100)
	s.settle()
}

// Two completions of the command list at once, while its source is read
// again and again: the shared decoded value is prepared for the query by
// every caller. This took the helper down with "concurrent map writes".
func TestHammerCompleteWhileCommandSourceRefreshes(t *testing.T) {
	ids := hammerIDs(1)
	s := newStorm(t, ids)
	complete := s.table[harness.MethodComplete]
	var wg sync.WaitGroup
	for g := 0; g < 24; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 150; i++ {
				raw, _ := json.Marshal(map[string]any{"id": ids[0], "kind": "command", "query": ""})
				res, err := complete(context.Background(), raw)
				if err != nil {
					s.failf("complete: %v", err)
					return
				}
				if c := res.(harness.CompleteResult); len(c.Items) < 2 {
					s.failf("complete returned %d items", len(c.Items))
					return
				}
			}
		}()
	}
	wg.Wait()
}
