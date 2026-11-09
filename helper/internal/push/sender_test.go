package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

var sprintf = fmt.Sprintf

func TestApprovalOpenPushedToEveryDevice(t *testing.T) {
	e := newEnv(t, 2)
	e.handle(approvalOpen("r_abcdefghij"))
	got := e.relay.received()
	if len(got) != 2 {
		t.Fatalf("relay got %d pushes, want 2", len(got))
	}
	// Devices are sent to side by side, so the relay sees them in any
	// order: each push must open for exactly one phone, and every phone
	// gets one.
	opened := map[string]bool{}
	for i, r := range got {
		if r.Kind != relayAlert || r.CollapseID != "r_abcdefghij" || r.Env != wire.EnvSandbox || r.Token != strings.Repeat("ab", 32) {
			t.Fatalf("push %d envelope fields %+v", i, r)
		}
		id := e.opener(t, r.Ciphertext)
		if opened[id] {
			t.Fatalf("%s got two pushes", id)
		}
		opened[id] = true
		p := e.open(t, r.Ciphertext, id)
		if p.Kind != wire.PushNeedsYou || p.RequestID != "r_abcdefghij" || len(p.Options) != 3 || p.Title != "Run npm test?" || p.Body != "Bash: npm test" {
			t.Fatalf("plaintext %+v", p)
		}
	}
}

// TestNeedsYouPlaintextMatchesFixture pins the sealed JSON to the shared
// example the phone decodes.
func TestNeedsYouPlaintextMatchesFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/fixtures/plaintext/push-needs-you-subject.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Plaintext json.RawMessage `json:"plaintext"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, 1)
	e.clock = time.UnixMilli(1793707200000)
	e.sender.Host = "studio"
	e.handle(withSubject(approvalOpen("r_abcdefghij"), wire.SubjectCommand, "npm test"))
	got := e.relay.received()
	if len(got) != 1 {
		t.Fatalf("relay got %d pushes", len(got))
	}
	keys, _ := e.svc.HostKeys()
	plain, err := openRaw(e.phones[0], got[0].Ciphertext, keys.HostID)
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

// TestNeedsYouAllowTextMatchesFixture: an approval that takes a typed answer
// says so in its push, so the phone can offer Reply; one that does not leaves
// the field out.
func TestNeedsYouAllowTextMatchesFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../protocol/v1/fixtures/plaintext/push.needs_you-text.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Plaintext json.RawMessage `json:"plaintext"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, 1)
	e.clock = time.UnixMilli(1793707200000)
	e.sender.Host = "studio"
	ev := withSubject(approvalOpen("r_abcdefghij"), wire.SubjectCommand, "npm test")
	a := ev.Payload.(wire.ApprovalOpenEvent)
	a.Approval.AllowText = true
	ev.Payload = a
	e.handle(ev)
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	got := e.relay.received()
	if len(got) != 2 {
		t.Fatalf("relay got %d pushes", len(got))
	}
	keys, _ := e.svc.HostKeys()
	plain, err := openRaw(e.phones[0], got[0].Ciphertext, keys.HostID)
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
	plain, err = openRaw(e.phones[0], got[1].Ciphertext, keys.HostID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "allowText") {
		t.Fatalf("buttons-only approval carries allowText: %s", plain)
	}
}

func TestNoPushWhileForegroundSubscriber(t *testing.T) {
	e := newEnv(t, 1)
	e.hub.fg.Store(1)
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	if n := len(e.relay.received()); n != 0 {
		t.Fatalf("relay got %d pushes while the app is in the foreground", n)
	}
	e.hub.fg.Store(0)
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	if n := len(e.relay.received()); n != 1 {
		t.Fatalf("relay got %d pushes after the app left the foreground", n)
	}
}

func TestMutedSessionNotPushed(t *testing.T) {
	e := newEnv(t, 1)
	e.muted["s_abcdefghij"] = true
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(notifyEv("s_abcdefghij", wire.LevelNeedsYou))
	if n := len(e.relay.received()); n != 0 {
		t.Fatalf("relay got %d pushes for a muted session", n)
	}
	e.handle(notifyEv("s_bbbbbbbbbb", wire.LevelNeedsYou))
	if n := len(e.relay.received()); n != 1 {
		t.Fatalf("other session: relay got %d pushes, want 1", n)
	}
}

func TestClearPushOnApprovalClosed(t *testing.T) {
	e := newEnv(t, 1)
	e.handle(wire.Event{Kind: wire.EventApprovalClosed, SessionID: sid("s_abcdefghij"),
		Payload: wire.ApprovalClosedEvent{RequestID: "r_abcdefghij", Resolution: wire.ResolutionDesk}})
	got := e.relay.received()
	if len(got) != 1 || got[0].Kind != relayBackground || got[0].CollapseID != "r_abcdefghij" {
		t.Fatalf("relay got %+v, want one background push collapsed on the request id", got)
	}
	p := e.open(t, got[0].Ciphertext, "dev_a")
	if p.Kind != wire.PushClear || p.RequestID != "r_abcdefghij" || p.Title != "" || p.Body != "" || p.Options != nil {
		t.Fatalf("clear plaintext %+v", p)
	}
}

func TestClearSentDespiteForegroundAndMute(t *testing.T) {
	e := newEnv(t, 1)
	e.hub.fg.Store(1)
	e.muted["s_abcdefghij"] = true
	e.handle(approvalOpen("r_abcdefghij"))
	if n := len(e.relay.received()); n != 0 {
		t.Fatalf("needs_you pushed while foreground and muted: %d", n)
	}
	e.handle(wire.Event{Kind: wire.EventApprovalClosed, SessionID: sid("s_abcdefghij"),
		Payload: wire.ApprovalClosedEvent{RequestID: "r_abcdefghij", Resolution: wire.ResolutionDesk}})
	got := e.relay.received()
	if len(got) != 1 || got[0].Kind != relayBackground {
		t.Fatalf("relay got %+v, want one clear push", got)
	}
	if p := e.open(t, got[0].Ciphertext, "dev_a"); p.Kind != wire.PushClear || p.RequestID != "r_abcdefghij" {
		t.Fatalf("clear plaintext %+v", p)
	}
}

func TestRateLimitPerSession(t *testing.T) {
	e := newEnv(t, 1)
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	e.handle(notifyEv("s_bbbbbbbbbb", wire.LevelDone))
	// Approvals are never rate limited.
	e.handle(approvalOpen("r_abcdefghij"))
	if n := len(e.relay.received()); n != 3 {
		t.Fatalf("relay got %d pushes, want 3 (one per session plus the approval)", n)
	}
	e.clock = e.clock.Add(sessionInterval)
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	if n := len(e.relay.received()); n != 4 {
		t.Fatalf("after the interval relay got %d pushes, want 4", n)
	}
}

func TestInfoNotifyAndRunningJobNotPushed(t *testing.T) {
	e := newEnv(t, 1)
	e.handle(notifyEv("s_abcdefghij", wire.LevelInfo))
	e.handle(wire.Event{Kind: wire.EventJob, Payload: wire.JobEvent{Job: wire.Job{JobID: "j_abcdefghij", Name: "build", Status: "running"}}})
	if n := len(e.relay.received()); n != 0 {
		t.Fatalf("relay got %d pushes", n)
	}
	e.handle(wire.Event{Kind: wire.EventJob, Payload: wire.JobEvent{Job: wire.Job{JobID: "j_abcdefghij", Name: "build", Status: "failed"}}})
	got := e.relay.received()
	if len(got) != 1 {
		t.Fatalf("job end: relay got %d pushes", len(got))
	}
	if p := e.open(t, got[0].Ciphertext, "dev_a"); p.Kind != wire.PushJob || p.Title != "build" || p.Body != "failed" || p.SessionID != nil {
		t.Fatalf("job plaintext %+v", p)
	}
}

func TestRelay410MarksDeviceDead(t *testing.T) {
	e := newEnv(t, 1)
	e.relay.status.Store(http.StatusGone)
	e.relay.answer.Store(`{"error":"unregistered"}`)
	e.handle(approvalOpen("r_abcdefghij"))
	list, err := e.svc.Devices().List()
	if err != nil || len(list) != 1 || !list[0].Dead {
		t.Fatalf("devices after 410: %+v, %v", list, err)
	}
	e.relay.status.Store(http.StatusAccepted)
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	if n := len(e.relay.received()); n != 1 {
		t.Fatalf("relay got %d pushes, want none after the device died", n)
	}
	// Pairing again with a fresh token revives it.
	e.pair(t, "dev_a", "")
	if list, _ := e.svc.Devices().List(); list[0].Dead {
		t.Fatal("re-paired device still dead")
	}
}

func TestRelay429PausesToken(t *testing.T) {
	e := newEnv(t, 1)
	e.relay.status.Store(http.StatusTooManyRequests)
	e.relay.answer.Store(`{"error":"rate_limited","retryAfter":30}`)
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	if n := len(e.relay.received()); n != 1 {
		t.Fatalf("relay got %d pushes, want 1 then a pause", n)
	}
	e.relay.status.Store(http.StatusAccepted)
	e.clock = e.clock.Add(31 * time.Second)
	e.handle(approvalOpen("r_cccccccccc"))
	if n := len(e.relay.received()); n != 2 {
		t.Fatalf("after retryAfter relay got %d pushes, want 2", n)
	}
}

func TestRelayServerErrorRetriedBoundedly(t *testing.T) {
	e := newEnv(t, 1)
	var waits []time.Duration
	e.sender.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	e.relay.status.Store(http.StatusBadGateway)
	e.relay.answer.Store(`{"error":"apns","reason":"InternalServerError"}`)
	e.handle(approvalOpen("r_abcdefghij"))
	if n := len(e.relay.received()); n != maxAttempts {
		t.Fatalf("relay got %d attempts, want %d", n, maxAttempts)
	}
	if len(waits) != maxAttempts-1 || waits[1] <= waits[0] {
		t.Fatalf("backoff waits %v", waits)
	}
	e.relay.status.Store(http.StatusBadRequest)
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	if n := len(e.relay.received()); n != maxAttempts+1 {
		t.Fatalf("400 was retried: %d attempts", n)
	}
}

func TestStubReceivesNoPlaintext(t *testing.T) {
	e := newEnv(t, 1)
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	bodies := e.relay.raw()
	if len(bodies) != 2 {
		t.Fatalf("relay got %d pushes", len(bodies))
	}
	for _, b := range bodies {
		for _, secret := range []string{"Run npm test", "npm test", "Yes, and", "Build", "finished", "claude", "s_abcdefghij"} {
			if bytes.Contains(b, []byte(secret)) {
				t.Fatalf("relay body contains %q: %s", secret, b)
			}
		}
	}
	if p := e.open(t, e.relay.received()[1].Ciphertext, "dev_a"); p.Kind != wire.PushDone || p.Title != "Build" {
		t.Fatalf("done plaintext %+v", p)
	}
	if strings.Contains(e.logs.String(), "ab"+"ab"+"ab") {
		t.Fatalf("logs contain a token: %s", e.logs)
	}
}

func TestLiveActivityUpdateCarriesMetadataOnly(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", strings.Repeat("cd", 32))
	e.handle(approvalOpen("r_abcdefghij"))
	got := e.relay.received()
	if len(got) != 2 || got[1].Kind != relayBackground {
		t.Fatalf("relay got %+v, want the live activity update and the silent copy for the app", got)
	}
	la := got[0]
	if la.Kind != relayLiveActivity || la.Token != strings.Repeat("cd", 32) || la.Ciphertext != "" || la.LiveActivity == nil {
		t.Fatalf("live activity push %+v", la)
	}
	cs := la.LiveActivity.ContentState
	if la.LiveActivity.Event != liveUpdate || cs.Waiting != 1 || cs.Status != wire.LevelNeedsYou || cs.Agent != "claude" || *cs.Bib != "01" {
		t.Fatalf("content state %+v", cs)
	}
	if bytes.Contains(e.relay.raw()[0], []byte("npm")) || got[1].Token == la.Token {
		t.Fatal("live activity push carries approval text")
	}
	e.handle(wire.Event{Kind: wire.EventApprovalClosed, SessionID: sid("s_abcdefghij"),
		Payload: wire.ApprovalClosedEvent{RequestID: "r_abcdefghij", Resolution: wire.ResolutionPhone}})
	live, _ := e.livePushes()
	if cs := live[len(live)-1].LiveActivity.ContentState; cs.Waiting != 0 || cs.Status == wire.LevelNeedsYou {
		t.Fatalf("after close content state %+v", cs)
	}
}

func TestOversizedPushDropsOptionsToFit(t *testing.T) {
	e := newEnv(t, 1)
	ev := approvalOpen("r_abcdefghij")
	a := ev.Payload.(wire.ApprovalOpenEvent)
	for n := 4; n < 60; n++ {
		a.Approval.Options = append(a.Approval.Options, wire.Option{N: n, Label: strings.Repeat("l", 80), Role: wire.RoleOther})
	}
	ev.Payload = a
	e.handle(ev)
	got := e.relay.received()
	if len(got) != 1 || len(got[0].Ciphertext) > maxCiphertext {
		t.Fatalf("relay got %d pushes", len(got))
	}
	if p := e.open(t, got[0].Ciphertext, "dev_a"); p.RequestID != "r_abcdefghij" || len(p.Options) == 0 || len(p.Options) >= 59 {
		t.Fatalf("trimmed plaintext has %d options", len(p.Options))
	}
}

func TestRunDeliversHubEvents(t *testing.T) {
	e := newEnv(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.sender.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.hub.mu.Lock()
		n := len(e.hub.fns)
		e.hub.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sender never subscribed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.hub.publish(approvalOpen("r_abcdefghij"))
	for len(e.relay.received()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no push delivered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestTruncateCountsCodePoints(t *testing.T) {
	if got := truncate(strings.Repeat("é", 300), wire.MaxTitle); len([]rune(got)) != wire.MaxTitle {
		t.Fatalf("truncate gave %d runes", len([]rune(got)))
	}
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate changed a short string: %q", got)
	}
}

func TestFallbackBodyIsCleaned(t *testing.T) {
	detail := "╭──────────────╮\n│ \x1b[31mHere is the plan:\x1b[0m │\n│ ❯ 1. Add   a‮ retry wrapper │\n╰──────────────╯\nthird\nfourth"
	s := &Sender{Hub: &fakeHub{}, Host: "studio", Now: time.Now}
	s.init()
	sid := "s_aaaaaaaaaa"
	ev := wire.Event{Kind: wire.EventApprovalOpen, SessionID: &sid, Payload: wire.ApprovalOpenEvent{Approval: wire.Approval{
		RequestID: "r_aaaaaaaaaa", Agent: "claude", Title: "Would you like to proceed?", Detail: &detail,
	}}}
	m, ok := s.message(ev, sid, SessionInfo{})
	if !ok {
		t.Fatal("no push")
	}
	if want := "Here is the plan:\n1. Add a retry wrapper\nthird"; m.Plaintext.Body != want {
		t.Fatalf("body = %q, want %q", m.Plaintext.Body, want)
	}
}

func TestMutedSendsOnlyClear(t *testing.T) {
	e := newEnv(t, 1)
	e.sender.Muted = func() bool { return true }
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(notifyEv("s_abcdefghij", wire.LevelNeedsYou))
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	if n := len(e.relay.received()); n != 0 {
		t.Fatalf("relay got %d pushes from a muted machine", n)
	}
	e.handle(wire.Event{Kind: wire.EventApprovalClosed, SessionID: sid("s_abcdefghij"),
		Payload: wire.ApprovalClosedEvent{RequestID: "r_abcdefghij", Resolution: wire.ResolutionDesk}})
	got := e.relay.received()
	if len(got) != 1 || e.open(t, got[0].Ciphertext, "dev_a").Kind != wire.PushClear {
		t.Fatalf("relay got %+v, want one clear", got)
	}
}

// A running Live Activity stands in for the push: the device with an update
// token gets only the activity update, with an alert; a device without one
// gets the regular alert push and no activity update. The same holds for
// done once nothing else waits or works.
func TestLiveActivityReplacesPushWhileRunning(t *testing.T) {
	e := newEnv(t, 1) // dev_a: no activity
	e.pair(t, "dev_b", strings.Repeat("cd", 32))
	e.handle(sessionEv("s_abcdefghij", "01", wire.LiveWorking))
	since := func(n int) (alerts int, live []relayPush) {
		for _, p := range e.relay.received()[n:] {
			switch p.Kind {
			case relayAlert:
				alerts++
			case relayLiveActivity:
				live = append(live, p)
			}
		}
		return alerts, live
	}
	check := func(n int, status string) {
		t.Helper()
		alerts, live := since(n)
		if alerts != 1 {
			t.Fatalf("%s: %d alert pushes, want 1 (the device without an activity)", status, alerts)
		}
		if len(live) != 1 || live[0].Token != strings.Repeat("cd", 32) || !live[0].LiveActivity.Alert ||
			live[0].LiveActivity.ContentState.Status != status {
			t.Fatalf("%s: live activity pushes %+v, want one alerting update", status, live)
		}
	}
	n := len(e.relay.received())
	e.handle(approvalOpen("r_abcdefghij"))
	check(n, wire.LevelNeedsYou)

	e.handle(approvalClosed("r_abcdefghij"))
	e.handle(sessionEv("s_abcdefghij", "01", "idle"))
	e.clock = e.clock.Add(time.Minute)
	n = len(e.relay.received())
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	check(n, wire.PushDone)
}

// A Live Activity the phone reports ended or dismissed no longer stands in
// for the push: the next request reaches that device as a regular alert.
func TestEndedLiveActivityFallsBackToPush(t *testing.T) {
	e := newEnv(t, 0)
	token := strings.Repeat("cd", 32)
	e.pair(t, "dev_b", token)
	e.handle(sessionEv("s_abcdefghij", "01", wire.LiveWorking))
	if err := e.svc.Devices().EndLiveActivity("dev_b", token); err != nil {
		t.Fatal(err)
	}
	n := len(e.relay.received())
	e.handle(approvalOpen("r_abcdefghij"))
	alerts := 0
	for _, p := range e.relay.received()[n:] {
		switch {
		case p.Kind == relayAlert:
			alerts++
		case p.Kind == relayLiveActivity && p.Token == token:
			t.Fatalf("update sent to the ended activity: %+v", p.LiveActivity)
		}
	}
	if alerts != 1 {
		t.Fatalf("%d alert pushes, want the regular push", alerts)
	}
}

// While a phone's Live Activity stands in for a request, nothing is mirrored
// to the Watch, so the Watch gets the request as an alert with its actions;
// otherwise it stays silent, and its clear always does.
func TestWatchAlertedWhileLiveActivityStandsIn(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_b", strings.Repeat("cd", 32))
	e.pairWith(t, "w_watch", func(p *wire.PairParams) { p.Platform = wire.PlatformWatch })
	e.handle(sessionEv("s_abcdefghij", "01", wire.LiveWorking))
	watch := func(n int) (kinds []string) {
		for _, p := range e.relay.received()[n:] {
			if p.Target == targetWatch {
				kinds = append(kinds, p.Kind)
			}
		}
		return kinds
	}
	n := len(e.relay.received())
	e.handle(approvalOpen("r_abcdefghij"))
	if got := watch(n); len(got) != 1 || got[0] != relayAlert {
		t.Fatalf("watch pushes %v, want one alert", got)
	}
	n = len(e.relay.received())
	e.handle(approvalClosed("r_abcdefghij"))
	if got := watch(n); len(got) != 1 || got[0] != relayBackground {
		t.Fatalf("watch pushes on clear %v, want one background", got)
	}

	// With no activity on the phone, its alert is mirrored: the Watch stays silent.
	if err := e.svc.Devices().EndLiveActivity("dev_b", strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}
	n = len(e.relay.received())
	e.handle(approvalOpen("r_bcdefghijk"))
	if got := watch(n); len(got) != 1 || got[0] != relayBackground {
		t.Fatalf("watch pushes without an activity %v, want one background", got)
	}
}

// A device that keeps done silent gets the done update standing in for its
// push without the alert, so it neither lights the screen nor sounds; a
// request still alerts.
func TestDoneSilentLiveActivityUpdateDoesNotAlert(t *testing.T) {
	e := newEnv(t, 0)
	token := strings.Repeat("cd", 32)
	e.pairWith(t, "dev_b", func(p *wire.PairParams) { p.LiveActivityToken, p.DoneSilent = token, true })
	e.handle(sessionEv("s_abcdefghij", "01", wire.LiveWorking))
	check := func(n int, status string, alert bool) {
		t.Helper()
		var live []relayPush
		for _, p := range e.relay.received()[n:] {
			switch p.Kind {
			case relayAlert:
				t.Fatalf("%s: regular push sent while the activity stands in", status)
			case relayLiveActivity:
				live = append(live, p)
			}
		}
		if len(live) != 1 || live[0].LiveActivity.ContentState.Status != status || live[0].LiveActivity.Alert != alert {
			t.Fatalf("%s: live pushes %+v, want one with alert %v", status, live, alert)
		}
	}
	n := len(e.relay.received())
	e.handle(approvalOpen("r_abcdefghij"))
	check(n, wire.LevelNeedsYou, true)

	e.handle(approvalClosed("r_abcdefghij"))
	e.handle(sessionEv("s_abcdefghij", "01", "idle"))
	e.clock = e.clock.Add(time.Minute)
	n = len(e.relay.received())
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	check(n, wire.PushDone, false)
}
