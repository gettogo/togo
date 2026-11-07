package push

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// remindEnv wires a Reminders to an env with a manual clock: timers fire
// when the test says so.
type remindEnv struct {
	*env
	rem     *Reminders
	delay   time.Duration
	pending map[string]bool
	timers  []*fakeTimer
}

type fakeTimer struct {
	d       time.Duration
	fn      func()
	stopped bool
}

func newRemindEnv(t *testing.T) *remindEnv {
	t.Helper()
	r := &remindEnv{env: newEnv(t, 1), delay: 10 * time.Minute, pending: map[string]bool{"r_abcdefghij": true}}
	r.sender.Host = "studio"
	r.rem = &Reminders{
		After:   func() time.Duration { return r.delay },
		Pending: func(id string) bool { return r.pending[id] },
		Now:     func() time.Time { return r.clock },
		AfterFunc: func(d time.Duration, fn func()) func() bool {
			ft := &fakeTimer{d: d, fn: fn}
			r.timers = append(r.timers, ft)
			return func() bool { was := !ft.stopped; ft.stopped = true; return was }
		},
	}
	r.sender.Remind = r.rem
	return r
}

// fire runs every timer not stopped, as if its delay passed.
func (r *remindEnv) fire() {
	for _, ft := range r.timers {
		if !ft.stopped {
			ft.stopped = true
			r.clock = r.clock.Add(ft.d)
			ft.fn()
		}
	}
}

func TestReminderOnceAfterDelay(t *testing.T) {
	r := newRemindEnv(t)
	r.handle(approvalOpen("r_abcdefghij"))
	if len(r.timers) != 1 || r.timers[0].d != 10*time.Minute {
		t.Fatalf("timers %+v", r.timers)
	}
	sid := "s_abcdefghij"
	r.rem.Schedule(Message{Plaintext: wire.PushPlaintext{Kind: wire.PushNeedsYou, RequestID: "r_abcdefghij", SessionID: &sid}})
	if len(r.timers) != 1 {
		t.Fatalf("a second schedule armed %d timers", len(r.timers))
	}
	r.fire()
	got := r.relay.received()
	if len(got) != 2 || got[1].Kind != relayAlert || got[1].CollapseID != "r_abcdefghij" {
		t.Fatalf("relay got %+v", got)
	}
	p := r.open(t, got[1].Ciphertext, r.phones[0].id)
	first := r.open(t, got[0].Ciphertext, r.phones[0].id)
	if !p.Reminder || first.Reminder || p.Kind != wire.PushNeedsYou || p.Title != first.Title || p.TS <= first.TS {
		t.Fatalf("reminder %+v first %+v", p, first)
	}
	r.fire()
	if n := len(r.relay.received()); n != 2 {
		t.Fatalf("%d pushes after a second pass, want 2", n)
	}
}

func TestNoReminderWhenAnswered(t *testing.T) {
	r := newRemindEnv(t)
	r.handle(approvalOpen("r_abcdefghij"))
	r.handle(approvalClosed("r_abcdefghij"))
	if !r.timers[0].stopped {
		t.Fatal("closing did not cancel the timer")
	}
	r.fire()
	// Closed by something that sent no clear: the pending check still holds.
	r.handle(approvalOpen("r_bbbbbbbbbb"))
	r.pending["r_bbbbbbbbbb"] = false
	r.fire()
	if n := len(r.relay.received()); n != 3 {
		t.Fatalf("%d pushes, want needs_you, clear, needs_you", n)
	}
}

func TestNoReminderWhenSessionEnds(t *testing.T) {
	r := newRemindEnv(t)
	r.handle(approvalOpen("r_abcdefghij"))
	r.handle(exitEv("s_abcdefghij", wire.StatusEnded, &wire.SessionExit{Code: code(0), Reason: wire.ExitReasonExit}))
	r.fire()
	if n := len(r.relay.received()); n != 2 {
		t.Fatalf("%d pushes, want needs_you and ended", n)
	}
}

func TestNoReminderWhenForeground(t *testing.T) {
	r := newRemindEnv(t)
	r.handle(approvalOpen("r_abcdefghij"))
	r.hub.fg.Store(1)
	r.fire()
	if n := len(r.relay.received()); n != 1 {
		t.Fatalf("%d pushes with an app in the foreground, want 1", n)
	}
}

func TestNoReminderWhenMutedOrOff(t *testing.T) {
	t.Run("machine muted", func(t *testing.T) {
		r := newRemindEnv(t)
		r.handle(approvalOpen("r_abcdefghij"))
		r.sender.Muted = func() bool { return true }
		r.fire()
		if n := len(r.relay.received()); n != 1 {
			t.Fatalf("%d pushes, want 1", n)
		}
	})
	t.Run("session muted", func(t *testing.T) {
		r := newRemindEnv(t)
		r.handle(approvalOpen("r_abcdefghij"))
		r.muted["s_abcdefghij"] = true
		r.fire()
		if n := len(r.relay.received()); n != 1 {
			t.Fatalf("%d pushes, want 1", n)
		}
	})
	t.Run("off", func(t *testing.T) {
		r := newRemindEnv(t)
		r.delay = 0
		r.handle(approvalOpen("r_abcdefghij"))
		if len(r.timers) != 0 {
			t.Fatalf("%d timers with reminders off", len(r.timers))
		}
	})
	t.Run("first push muted", func(t *testing.T) {
		r := newRemindEnv(t)
		r.sender.Muted = func() bool { return true }
		r.handle(approvalOpen("r_abcdefghij"))
		if len(r.timers) != 0 {
			t.Fatalf("%d timers though nothing was delivered", len(r.timers))
		}
	})
}

func TestReminderPlaintextMatchesFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/fixtures/plaintext/push.reminder.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Plaintext json.RawMessage `json:"plaintext"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	r := newRemindEnv(t)
	r.clock = time.UnixMilli(1793707200000)
	r.handle(withSubject(approvalOpen("r_abcdefghij"), wire.SubjectCommand, "npm test"))
	r.fire()
	got := r.relay.received()
	keys, _ := r.svc.HostKeys()
	plain, err := openRaw(r.phones[0], got[1].Ciphertext, keys.HostID)
	if err != nil {
		t.Fatal(err)
	}
	var gotJSON, want any
	if err := json.Unmarshal(plain, &gotJSON); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fx.Plaintext, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotJSON, want) {
		t.Fatalf("plaintext\n got %s\nwant %s", plain, fx.Plaintext)
	}
}

// A new process has no timers: nothing is stored, so a request open across
// a restart is never reminded about.
func TestDaemonRestartDoesNotRemind(t *testing.T) {
	r := newRemindEnv(t)
	r.handle(approvalOpen("r_abcdefghij"))
	fresh := newRemindEnv(t)
	fresh.rem.Schedule(Message{}) // not a needs_you: ignored
	fresh.fire()
	if len(fresh.timers) != 0 || len(fresh.relay.received()) != 0 {
		t.Fatalf("a new process armed %d timers", len(fresh.timers))
	}
	if entries, _ := os.ReadDir(r.svc.Home); len(entries) > 0 {
		for _, e := range entries {
			if e.Name() == "reminders.json" {
				t.Fatal("timers were written to disk")
			}
		}
	}
}

func TestDuplicateNeedsYouAfterReminderArmsNothing(t *testing.T) {
	r := newRemindEnv(t)
	r.handle(approvalOpen("r_abcdefghij"))
	r.fire()
	r.handle(approvalOpen("r_abcdefghij"))
	r.fire()
	got := r.relay.received()
	// The repeated open is the same request: it neither rings nor re-arms.
	if len(got) != 2 {
		t.Fatalf("%d pushes, want needs_you, reminder", len(got))
	}
	if !r.open(t, got[1].Ciphertext, r.phones[0].id).Reminder {
		t.Fatal("second push is not the reminder")
	}
	// A closed request never rings or arms again.
	r.handle(approvalClosed("r_abcdefghij"))
	r.handle(approvalOpen("r_abcdefghij"))
	if live := liveTimers(r); live != 0 {
		t.Fatalf("%d live timers after close and reopen, want 0", live)
	}
}

func liveTimers(r *remindEnv) int {
	n := 0
	for _, ft := range r.timers {
		if !ft.stopped {
			n++
		}
	}
	return n
}

func TestChangedDelayReevaluatesArmedReminders(t *testing.T) {
	r := newRemindEnv(t)
	r.handle(approvalOpen("r_abcdefghij"))
	r.clock = r.clock.Add(4 * time.Minute)
	r.delay = 15 * time.Minute
	r.rem.Reschedule()
	if !r.timers[0].stopped || len(r.timers) != 2 || r.timers[1].d != 11*time.Minute {
		t.Fatalf("timers %+v %+v", r.timers[0], r.timers[len(r.timers)-1])
	}
	r.fire()
	if n := len(r.relay.received()); n != 2 {
		t.Fatalf("%d pushes, want 2 (the stopped timer must not also fire)", n)
	}
	// A delay already passed fires at once; off cancels.
	r.handle(approvalClosed("r_abcdefghij"))
	r.handle(approvalOpen("r_bbbbbbbbbb"))
	r.clock = r.clock.Add(20 * time.Minute)
	r.delay = 5 * time.Minute
	r.rem.Reschedule()
	if last := r.timers[len(r.timers)-1]; last.d != 0 {
		t.Fatalf("overdue delay %v, want 0", last.d)
	}
	r.delay = 0
	r.rem.Reschedule()
	if liveTimers(r) != 0 {
		t.Fatal("off left a timer armed")
	}
}

func TestNoReminderWhenSessionRemoved(t *testing.T) {
	r := newRemindEnv(t)
	r.handle(approvalOpen("r_abcdefghij"))
	r.handle(wire.Event{Kind: wire.EventSessionRemoved, SessionID: sid("s_abcdefghij"), Payload: wire.SessionRemovedEvent{}})
	if liveTimers(r) != 0 {
		t.Fatal("removal left the timer armed")
	}
}
