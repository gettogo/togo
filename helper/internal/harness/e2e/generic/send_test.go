package generic

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Sending, queueing, taking back, interrupting and approvals, per agent.

func TestSend(t *testing.T) {
	cases := []struct {
		name, cell string
		run        func(t *testing.T, lv *live)
	}{
		{"idle", "capability/chat", sendIdle},
		{"queue_and_take_back", "capability/send.queue", queueAndTakeBack},
		{"interrupt", "capability/interrupt", interruptKeys},
		{"approval", "capability/needs.approvals", approvalFromPhone},
		{"storm", "scale/concurrent_requests", storm},
	}
	for _, agent := range profiles(t) {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					t.Parallel()
					if !declare(t, agent, c.cell) {
						return
					}
					c.run(t, start(t, agent))
				})
			}
		})
	}
}

// sendIdle: a prompt sent while the agent is idle is typed at once and
// given to the agent exactly as written.
func sendIdle(t *testing.T, lv *live) {
	r := lv.r
	lv.idle()
	text := "Rename task-management-service to tasks; keep primary_key=True and <div> as they are, 100%!"
	res, err := r.Send(text, harness.SendNow)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Item.User == nil || res.Item.User.Queued || res.Item.User.Text != text {
		t.Errorf("send result %+v", res.Item.User)
	}
	if got := r.Term.Submitted(); !slices.Equal(got, []string{text}) {
		t.Errorf("the agent was given %q", got)
	}
}

// queueAndTakeBack: two prompts sent while the agent works are queued in
// order; the first is taken back; when the turn ends only the second is
// typed, once. A prompt asked to go now is refused as busy.
func queueAndTakeBack(t *testing.T, lv *live) {
	r := lv.r
	r.SetHubStatus("working")
	if _, err := r.Send("Now please", harness.SendNow); !busy(err) {
		t.Errorf("send now while working: %v, want busy", err)
	}
	a, err := r.Send("First queued", harness.SendQueue)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Send("Second queued", harness.SendQueue)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Item.User.Queued || !b.Item.User.Queued {
		t.Fatalf("not queued: %+v %+v", a.Item.User, b.Item.User)
	}
	if res, err := r.TakeBack(a.Item.ID); err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("take back: %+v %v", res, err)
	}
	if res, _ := r.TakeBack(a.Item.ID); res.Result == harness.ResultApplied {
		t.Error("a prompt taken back twice was taken back again")
	}
	lv.endTurn()
	r.SetHubStatus("idle")
	eventually(t, 10*time.Second, "the second prompt to be typed", func() bool {
		return slices.Contains(r.Term.Submitted(), "Second queued")
	})
	time.Sleep(200 * time.Millisecond)
	if got := r.Term.Submitted(); !slices.Equal(got, []string{"Second queued"}) {
		t.Errorf("the agent was given %q", got)
	}
	st := r.Settle(3 * time.Second)
	if it, _ := item(st, a.Item.ID); it.User == nil || !it.User.Withdrawn {
		t.Errorf("the taken-back prompt is not withdrawn: %+v", it.User)
	}
	if res, _ := r.TakeBack(b.Item.ID); res.Result != harness.ResultFailed {
		t.Errorf("taking back a prompt already typed gave %+v", res)
	}
}

// interruptKeys: the phone's interrupt reaches the pane as the adapter's
// interrupt key, and the turn the agent then ends, and its tools, are
// not running. Background subagents may run on: the agents keep them
// through an interrupt of the main turn.
func interruptKeys(t *testing.T, lv *live) {
	r := lv.r
	r.SetHubStatus("working")
	var once sync.Once
	r.Term.OnInterrupt = func(string) { go once.Do(func() { lv.endTurn(); r.SetHubStatus("idle") }) }
	res, err := r.Interrupt()
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if res.Result == harness.ResultFailed || res.Result == harness.ResultFallback {
		t.Fatalf("interrupt: %+v", res)
	}
	if got, ok := interruptSent(r); !ok {
		t.Fatalf("no interrupt reached the agent: %v", got)
	}
	st := r.Settle(3 * time.Second)
	for _, it := range st.Items {
		if latest, _ := item(st, it.ID); latest.Seq == it.Seq && (it.Tool != nil && running(it.Tool.State) || it.Turn != nil && running(it.Turn.State)) {
			t.Errorf("%s is still running after the interrupt", it.ID)
		}
	}
}

// approvalFromPhone: the agent's permission prompt (a real capture)
// reaches Needs you; a prompt sent meanwhile waits behind it and one
// asked to go now is refused with the reason; the phone's answer is typed
// into the pane, the prompt leaves, the approval closes as the phone's,
// and the waiting prompt goes. An answer after that is refused. A
// prompt that leaves the screen unanswered expires.
func approvalFromPhone(t *testing.T, lv *live) {
	r := lv.r
	rel := dialogFor(lv.agent)
	if rel == "" && r.API() != nil {
		// Its permission prompts reach the phone from its API, not from
		// the screen; the agent's own scenarios answer them there.
		t.Skipf("%s: no captured permission screen; its API permissions are tested in e2e (TestKiloPermissionsAndQuestions)", lv.agent)
	}
	if rel == "" {
		t.Fatalf("%s declares approvals and has no captured permission prompt in detect/testdata", lv.agent)
	}
	lv.idle()
	r.Term.Open(&enginetest.Dialog{Screen: r.Term.MustCapture(rel)})
	r.Look()
	p := r.WaitPending(1, 3*time.Second)[0]
	if _, err := r.Send("Typed into the dialog?", harness.SendNow); err == nil {
		t.Fatal("a prompt was sent now while the approval was open")
	}
	q, err := r.Send("After the approval", harness.SendQueue)
	if err != nil || !q.Item.User.Queued {
		t.Fatalf("queue behind the approval: %+v %v", q.Item.User, err)
	}
	if err := r.Answer(p.RequestID, wire.DecisionApprove); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if r.Term.Dialog() != nil {
		t.Fatal("the prompt is still on screen after the phone's answer")
	}
	if got := r.Closed()[p.RequestID]; got != wire.ByPhone {
		t.Errorf("closed as %q, want %q", got, wire.ByPhone)
	}
	r.Look()
	eventually(t, 10*time.Second, "the waiting prompt to be typed", func() bool {
		return slices.Contains(r.Term.Submitted(), "After the approval")
	})
	if err := r.Answer(p.RequestID, wire.DecisionDeny); err == nil {
		t.Error("a second answer to a closed approval was taken")
	}
	if slices.Contains(r.Term.Submitted(), "Typed into the dialog?") {
		t.Error("the refused prompt was typed")
	}

	// Expiry: a rig whose approvals live briefly, the prompt gone from
	// the screen without the watcher seeing it go.
	x := enginetest.New(t, enginetest.Options{Agent: lv.agent, Home: r.Opt.Home, Cwd: r.Opt.Cwd, Created: r.Opt.Created,
		ID: enginetest.NewID(), ApprovalTTL: 300 * time.Millisecond, NoRedact: true})
	x.Term.Open(&enginetest.Dialog{Screen: x.Term.MustCapture(rel)})
	x.Look()
	e := x.WaitPending(1, 3*time.Second)[0]
	time.Sleep(700 * time.Millisecond)
	if len(x.Pending()) != 1 {
		t.Error("an approval whose prompt is still on screen expired")
	}
	x.Term.CloseDialog()
	eventually(t, 3*time.Second, "the approval to expire", func() bool { return len(x.Pending()) == 0 })
	if got := x.Closed()[e.RequestID]; got != wire.ResolutionExpired {
		t.Errorf("closed as %q, want expired", got)
	}
	if err := x.Answer(e.RequestID, wire.DecisionApprove); err == nil {
		t.Error("an expired approval took an answer")
	}
	if len(x.Term.KeysSent()) != 0 {
		t.Errorf("keys reached the pane for an expired approval: %v", x.Term.KeysSent())
	}
}

func busy(err error) bool {
	var ce *api.CodeError
	return errors.As(err, &ce) && ce.Code == rpc.CodeConflict
}

// storm: many clients at once against one session of the agent while it
// works: prompts queued, reads, history, describes, completions and take
// backs. Every call returns; the queue never holds more than its bound;
// every prompt accepted and not taken back is typed once, in the order the
// engine queued it, when the turn ends. That order is the order of the
// queued items' Seq (engine/send.go queues and numbers a prompt under one
// lock); the order the clients' calls returned in is not it: two calls
// can return in the other order than they were queued.
func storm(t *testing.T, lv *live) {
	r := lv.r
	r.SetHubStatus("working")
	const clients, rounds = 12, 6
	var mu sync.Mutex
	var accepted []harness.Item
	taken := map[string]bool{}
	byID := map[string]string{}
	var wg sync.WaitGroup
	for c := range clients {
		wg.Go(func() {
			for i := range rounds {
				switch (c + i) % 6 {
				case 0, 1:
					text := fmt.Sprintf("Prompt %d.%d", c, i)
					res, err := r.Send(text, harness.SendQueue)
					if err != nil {
						if !busy(err) {
							t.Errorf("queue: %v", err)
						}
						continue
					}
					mu.Lock()
					accepted = append(accepted, res.Item)
					byID[res.Item.ID] = text
					mu.Unlock()
				case 2:
					_ = r.ReadPages(7)
				case 3:
					_ = r.Describe()
				case 4:
					if _, err := r.Complete(harness.CompleteCommand, ""); err != nil && !busy(err) {
						var ce *api.CodeError
						if !errors.As(err, &ce) {
							t.Errorf("complete: %v", err)
						}
					}
				case 5:
					mu.Lock()
					var id string
					for k := range byID {
						if !taken[k] {
							id = k
							break
						}
					}
					if id != "" {
						taken[id] = true
					}
					mu.Unlock()
					if id != "" {
						if res, err := r.TakeBack(id); err != nil || res.Result != harness.ResultApplied {
							mu.Lock()
							delete(taken, id)
							mu.Unlock()
						}
					}
				}
			}
		})
	}
	wg.Wait()
	st := r.Read()
	queued := 0
	for _, it := range st.Items {
		if latest, _ := item(st, it.ID); latest.Seq == it.Seq && it.User != nil && it.User.Queued {
			queued++
		}
	}
	if queued > engine.MaxQueued {
		t.Errorf("%d prompts queued, the bound is %d", queued, engine.MaxQueued)
	}
	slices.SortFunc(accepted, func(a, b harness.Item) int { return int(a.Seq - b.Seq) })
	var want []string
	for _, it := range accepted {
		if !taken[it.ID] {
			want = append(want, it.User.Text)
		}
	}
	lv.endTurn()
	r.SetHubStatus("idle")
	eventually(t, 30*time.Second, "every queued prompt to be typed", func() bool {
		return len(r.Term.Submitted()) >= len(want)
	})
	time.Sleep(300 * time.Millisecond)
	if got := r.Term.Submitted(); !slices.Equal(got, want) {
		t.Errorf("typed %q,\nwant %q", got, want)
	}
	enginetest.Fail(t, "after the storm", enginetest.Paging(r.Read()))
}
