package push

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestLiveHeldUpdateMaskedForCurrentSetting(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) {
		p.LiveActivityToken = updateToken
		p.LiveActivityCommand = true
	})
	e.handle(sessionEv("s_aaaaaaaaaa", "02", "working"))
	// Hold an update that carries command text, as if queued while the
	// device had opted in.
	cmd := "rm -rf ./build"
	e.sender.liveMu.Lock()
	held := e.sender.liveDev["dev_a"].last
	held.Command = &cmd
	e.sender.liveDev["dev_a"].pending = &held
	e.sender.liveMu.Unlock()
	// Opting out pairs again before the window passes.
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityToken = updateToken })
	e.clock = e.clock.Add(liveCoalesce)
	e.tick()
	la, raw := e.livePushes()
	if len(la) != 2 {
		t.Fatalf("got %d live activity pushes, want the held update", len(la))
	}
	if la[1].LiveActivity.ContentState.Command != nil || bytes.Contains(raw[1], []byte("rm -rf")) {
		t.Fatalf("held update kept command text after the opt-out: %s", raw[1])
	}
	if !bytes.Contains(raw[1], []byte(`"command":null`)) {
		t.Fatalf("command not null: %s", raw[1])
	}
}

func TestLiveAgentFallsBackToShell(t *testing.T) {
	e := newEnv(t, 0)
	e.sender.Session = nil
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	ev := approvalOpen("r_abcdefghij")
	a := ev.Payload.(wire.ApprovalOpenEvent)
	a.Approval.Agent = ""
	ev.Payload = a
	e.handle(ev)
	la, _ := e.livePushes()
	if len(la) != 1 || la[0].LiveActivity.Event != liveStart || la[0].LiveActivity.ContentState.Agent != agentShell {
		t.Fatalf("start with unknown agent: %+v", la)
	}
	tr := &LiveTracker{}
	tr.Session("s_x", wire.LiveWorking, nil, "", "", e.clock)
	if st, _, ok := tr.Next(e.clock); !ok || st.Agent != agentShell || st.Status != wire.LiveWorking {
		t.Fatalf("working session without agent: %+v ok=%v", st, ok)
	}
}

func TestUpdateTokensRefusesOlderRegistration(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", "")
	d := e.svc.Devices()
	newer, older := strings.Repeat("11", 32), strings.Repeat("22", 32)
	if err := d.UpdateTokens("dev_a", "", newer, "", false, 2000); err != nil {
		t.Fatal(err)
	}
	for _, ts := range []int64{1000, 2000} {
		if err := d.UpdateTokens("dev_a", "", older, "", false, ts); err != ErrStaleRegistration {
			t.Fatalf("ts %d: %v", ts, err)
		}
	}
	// The clock is kept in the store and survives pairing again.
	e.pair(t, "dev_a", "")
	fresh := &Devices{Home: e.svc.Home}
	list, _ := fresh.List()
	if list[0].RegisterTS != 2000 {
		t.Fatalf("RegisterTS after re-pair %d", list[0].RegisterTS)
	}
	if err := fresh.UpdateTokens("dev_a", "", older, "", false, 1500); err != ErrStaleRegistration {
		t.Fatalf("after restart: %v", err)
	}
}

func TestLiveFailedStartRetriedOnce(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	starts := func() int {
		n := 0
		la, _ := e.livePushes()
		for _, p := range la {
			if p.LiveActivity.Event == liveStart {
				n++
			}
		}
		return n
	}
	e.relay.status.Store(http.StatusBadRequest)
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	if n := starts(); n != 2 {
		t.Fatalf("got %d start attempts, want the first and one retry", n)
	}
	e.handle(approvalOpen("r_cccccccccc"))
	if n := starts(); n != 2 {
		t.Fatalf("retried more than once: %d attempts", n)
	}

	// A retry that succeeds counts as started.
	e2 := newEnv(t, 0)
	e2.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e2.relay.status.Store(http.StatusBadRequest)
	e2.handle(approvalOpen("r_aaaaaaaaaa"))
	e2.relay.status.Store(http.StatusAccepted)
	e2.handle(approvalOpen("r_bbbbbbbbbb"))
	e2.handle(approvalOpen("r_cccccccccc"))
	la, _ := e2.livePushes()
	if len(la) != 2 || la[1].LiveActivity.Event != liveStart || la[1].LiveActivity.ContentState.Waiting != 2 {
		t.Fatalf("retry pushes %+v", la)
	}
}

// A helper restart keeps the update token of the activity running on the
// phone, so the next request updates it instead of starting a second one.
func TestRunKeepsLiveTokensAcrossARestart(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) {
		p.LiveActivityToken = updateToken
		p.LiveActivityStartToken = startToken
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.sender.Run(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
	list, _ := (&Devices{Home: e.svc.Home}).List()
	if list[0].LiveActivityToken != updateToken || list[0].LiveActivityStartToken != startToken {
		t.Fatalf("tokens after a restart: %+v", list[0].Device)
	}
	e.sender.process(context.Background(), approvalOpen("r_abcdefghij"))
	la, _ := e.livePushes()
	if len(la) != 1 || la[0].Token != updateToken || la[0].LiveActivity.Event != liveUpdate {
		t.Fatalf("after restart: %+v", la)
	}
}

func TestLiveGoneUpdateTokenFallsBackToStart(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) {
		p.LiveActivityToken = updateToken
		p.LiveActivityStartToken = startToken
	})
	// With the app in the foreground only Live Activity pushes go out, so
	// the 410 below answers the update token alone.
	e.hub.fg.Store(1)
	e.handle(approvalOpen("r_aaaaaaaaaa"))
	e.relay.status.Store(http.StatusGone)
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	e.relay.status.Store(http.StatusAccepted)
	if list, _ := e.svc.Devices().List(); list[0].Dead || list[0].LiveActivityToken != "" {
		t.Fatalf("after 410: dead=%v token kept=%v", list[0].Dead, list[0].LiveActivityToken != "")
	}
	e.handle(approvalOpen("r_cccccccccc"))
	la, _ := e.livePushes()
	last := la[len(la)-1]
	if len(la) != 3 || last.Token != startToken || last.LiveActivity.Event != liveStart || last.LiveActivity.ContentState.Waiting != 3 {
		t.Fatalf("after 410 pushes %+v", la)
	}
}

func TestLiveSendsCurrentStateToNewToken(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(approvalOpen("r_abcdefghij"))
	if err := e.svc.Devices().UpdateTokens("dev_a", "", updateToken, "", false, 1); err != nil {
		t.Fatal(err)
	}
	e.clock = e.clock.Add(time.Second)
	e.tick()
	la, _ := e.livePushes()
	if len(la) != 2 {
		t.Fatalf("got %d pushes, want start then the current state", len(la))
	}
	p := la[1].LiveActivity
	if la[1].Token != updateToken || p.Event != liveUpdate || p.ContentState.Waiting != 1 ||
		p.StaleDate == nil || *p.StaleDate != p.ContentState.Since+600 {
		t.Fatalf("replayed state %+v", la[1])
	}
}

func TestLiveCommandOnlyFromCommandSubjects(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_b", func(p *wire.PairParams) {
		p.LiveActivityToken = strings.Repeat("12", 32)
		p.LiveActivityCommand = true
	})
	e.handle(withSubject(approvalOpen("r_abcdefghij"), wire.SubjectFile, "/etc/hosts"))
	_, raw := e.livePushes()
	if len(raw) != 1 {
		t.Fatalf("got %d live activity pushes", len(raw))
	}
	if bytes.Contains(raw[0], []byte("/etc/hosts")) {
		t.Fatalf("a file subject went out as command text: %s", raw[0])
	}
}

func TestLiveCommandFitsInUTF16Units(t *testing.T) {
	got := truncateUTF16(strings.Repeat("😀", maxLiveCommand), maxLiveCommand)
	if n := len(utf16.Encode([]rune(got))); n > maxLiveCommand || n < maxLiveCommand-1 {
		t.Fatalf("command is %d UTF-16 units", n)
	}
	if !utf8.ValidString(got) {
		t.Fatal("cut inside a scalar")
	}
	if s := truncateUTF16("rm -rf ./build", maxLiveCommand); s != "rm -rf ./build" {
		t.Fatalf("short command changed: %q", s)
	}
}

func TestWatchGetsOnlyNeedsYouAndClear(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "w_watch", func(p *wire.PairParams) { p.Platform = wire.PlatformWatch })
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	if got := e.relay.received(); len(got) != 0 {
		t.Fatalf("watch got %d pushes for a notify", len(got))
	}
}

func TestRegisteredCommandChoiceApplies(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) {
		p.LiveActivityToken = updateToken
		p.LiveActivityCommand = true
	})
	if err := e.svc.Devices().UpdateTokens("dev_a", "", "", "", false, 1); err != nil {
		t.Fatal(err)
	}
	e.handle(withSubject(approvalOpen("r_abcdefghij"), wire.SubjectCommand, "rm -rf ./build"))
	_, raw := e.livePushes()
	if len(raw) != 1 || bytes.Contains(raw[0], []byte("rm -rf")) {
		t.Fatalf("command text sent after the device turned it off: %d pushes", len(raw))
	}
	list, _ := e.svc.Devices().List()
	if list[0].LiveActivityCommand || list[0].LiveActivityToken != updateToken {
		t.Fatalf("stored device %+v", list[0])
	}
}

// Command text in a Live Activity goes only sealed, and redacted.
func TestLiveCommandIsRedacted(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_b", func(p *wire.PairParams) {
		p.LiveActivityToken = strings.Repeat("12", 32)
		p.LiveActivityCommand = true
	})
	e.handle(withSubject(approvalOpen("r_abcdefghij"), wire.SubjectCommand, `curl -H "Authorization: Bearer abcdefghijklmnopq123" https://x`))
	_, raw := e.livePushes()
	if len(raw) != 1 {
		t.Fatalf("got %d live activity pushes", len(raw))
	}
	if bytes.Contains(raw[0], []byte("abcdefghijklmnopq123")) || bytes.Contains(raw[0], []byte("curl")) {
		t.Fatalf("command went out in plain: %s", raw[0])
	}
	la, _ := e.livePushes()
	if c := e.liveCtx(t, la[0], "dev_b"); c.Subject == nil || strings.Contains(c.Subject.Text, "abcdefghijklmnopq123") || !strings.Contains(c.Subject.Text, "curl") {
		t.Fatalf("sealed subject %+v", c.Subject)
	}
}

// The update that clears an answered request is urgent, so the relay
// sends it at priority 10 while routine progress goes at 5.
func TestAnsweredRequestClearIsUrgent(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", updateToken)
	e.handle(approvalOpen("r_abcdefghij"))
	e.clock = e.clock.Add(10 * time.Second)
	e.handle(approvalClosed("r_abcdefghij"))
	la, raw := e.livePushes()
	if len(la) < 2 {
		t.Fatalf("got %d live pushes", len(la))
	}
	if la[0].LiveActivity.Urgent || !la[len(la)-1].LiveActivity.Urgent {
		t.Fatalf("urgent flags: open %v, clear %v (%s)", la[0].LiveActivity.Urgent, la[len(la)-1].LiveActivity.Urgent, raw[len(raw)-1])
	}
}
