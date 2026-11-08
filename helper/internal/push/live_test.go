package push

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

var (
	startToken  = strings.Repeat("ef", 32)
	updateToken = strings.Repeat("cd", 32)
)

func withSubject(ev wire.Event, kind, text string) wire.Event {
	a := ev.Payload.(wire.ApprovalOpenEvent)
	a.Approval.Subject = &wire.Subject{Kind: kind, Text: text}
	a.Approval.Detail = &text
	ev.Payload = a
	return ev
}

func approvalClosed(rid string) wire.Event {
	return wire.Event{Kind: wire.EventApprovalClosed, SessionID: sid("s_abcdefghij"),
		Payload: wire.ApprovalClosedEvent{RequestID: rid, Resolution: wire.ResolutionPhone}}
}

func sessionEv(id, bib, status string) wire.Event {
	return wire.Event{Kind: wire.EventSession, SessionID: sid(id), Payload: wire.SessionEvent{Session: session.Session{
		ID: id, Bib: sid(bib), Agent: "codex", Status: status, Managed: true,
	}}}
}

// livePushes returns the Live Activity requests the relay got, decoded and
// raw, in order.
func (e *env) livePushes() ([]relayPush, [][]byte) {
	var out []relayPush
	var raw [][]byte
	bodies := e.relay.raw()
	for i, p := range e.relay.received() {
		if p.Kind == relayLiveActivity {
			out = append(out, p)
			raw = append(raw, bodies[i])
		}
	}
	return out, raw
}

func TestLiveStartOnFirstNeedsYou(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(approvalOpen("r_abcdefghij"))
	la, _ := e.livePushes()
	if len(la) != 1 {
		t.Fatalf("got %d live activity pushes, want one start", len(la))
	}
	keys, _ := e.svc.HostKeys()
	p := la[0]
	if p.Token != startToken || p.LiveActivity.Event != liveStart || p.Ciphertext != "" || p.Target != "" {
		t.Fatalf("start push %+v", p)
	}
	if p.LiveActivity.Attributes == nil || p.LiveActivity.Attributes.HostID != keys.HostID {
		t.Fatalf("start attributes %+v", p.LiveActivity.Attributes)
	}
	cs := p.LiveActivity.ContentState
	if cs.Status != wire.LevelNeedsYou || cs.Waiting != 1 || cs.RequestID == nil || *cs.RequestID != "r_abcdefghij" ||
		cs.Since != time.Date(2026, 11, 3, 11, 59, 0, 0, time.UTC).Unix() || cs.Agent != "claude" || *cs.Bib != "01" {
		t.Fatalf("start content state %+v", cs)
	}
	if p.LiveActivity.StaleDate == nil || *p.LiveActivity.StaleDate != e.clock.Unix()+3600 {
		t.Fatalf("staleDate %v", p.LiveActivity.StaleDate)
	}
	// No update token yet: a second request starts nothing new.
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	if la, _ := e.livePushes(); len(la) != 1 {
		t.Fatalf("got %d live activity pushes after a second request", len(la))
	}
}

// A muted session's request never starts the Live Activity (a tester's
// muted sessions must stay off the owner's Dynamic Island).
func TestLiveSkipsMutedSession(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.muted["s_abcdefghij"] = true
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(sessionEv("s_abcdefghij", "01", wire.LevelNeedsYou))
	if la, _ := e.livePushes(); len(la) != 0 {
		t.Fatalf("got %d live activity pushes for a muted session", len(la))
	}
}

func TestLiveUpdatesUseUpdateTokenAfterRegister(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(approvalOpen("r_abcdefghij"))
	if err := e.svc.Devices().UpdateTokens("dev_a", "", updateToken, "", false, 1); err != nil {
		t.Fatal(err)
	}
	e.clock = e.clock.Add(time.Second)
	e.handle(approvalOpen("r_bbbbbbbbbb"))
	la, _ := e.livePushes()
	last := la[len(la)-1]
	if last.Token != updateToken || last.LiveActivity.Event != liveUpdate || last.LiveActivity.ContentState.Waiting != 2 {
		t.Fatalf("after register: %+v", last)
	}
	if last.LiveActivity.Attributes != nil {
		t.Fatal("update carries start attributes")
	}
	list, _ := e.svc.Devices().List()
	if list[0].APNsToken != strings.Repeat("ab", 32) || list[0].LiveActivityStartToken != startToken {
		t.Fatal("empty fields changed stored tokens")
	}
}

func TestLiveEndAfterTenQuietMinutes(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", updateToken)
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(approvalClosed("r_abcdefghij"))
	quiet := e.clock
	e.clock = quiet.Add(9 * time.Minute)
	e.tick()
	la, _ := e.livePushes()
	for _, p := range la {
		if p.LiveActivity.Event == liveEnd {
			t.Fatal("ended before ten quiet minutes")
		}
	}
	if cs := la[len(la)-1].LiveActivity.ContentState; cs.Status != wire.PushDone || cs.Waiting != 0 || cs.RequestID != nil {
		t.Fatalf("after close content state %+v", cs)
	}
	e.clock = quiet.Add(10 * time.Minute)
	e.tick()
	la, _ = e.livePushes()
	end := la[len(la)-1]
	if end.LiveActivity.Event != liveEnd || end.Token != updateToken {
		t.Fatalf("last push %+v, want end", end)
	}
	if end.LiveActivity.StaleDate == nil || *end.LiveActivity.StaleDate != e.clock.Unix()+3600 {
		t.Fatalf("end staleDate %v", end.LiveActivity.StaleDate)
	}
	if end.LiveActivity.DismissalDate == nil || *end.LiveActivity.DismissalDate != e.clock.Unix()+900 {
		t.Fatalf("dismissalDate %v", end.LiveActivity.DismissalDate)
	}
	e.clock = e.clock.Add(time.Minute)
	e.tick()
	if n, _ := e.livePushes(); len(n) != len(la) {
		t.Fatal("pushed again after the end")
	}
	if list, _ := e.svc.Devices().List(); list[0].LiveActivityToken != "" {
		t.Fatal("ended activity's token still stored")
	}
}

func TestLiveCoalescesWorkingUpdates(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", updateToken)
	e.handle(sessionEv("s_aaaaaaaaaa", "02", "working"))
	e.clock = e.clock.Add(500 * time.Millisecond)
	e.handle(sessionEv("s_bbbbbbbbbb", "03", "working"))
	e.clock = e.clock.Add(500 * time.Millisecond)
	e.handle(sessionEv("s_aaaaaaaaaa", "02", "idle"))
	e.tick()
	la, _ := e.livePushes()
	if len(la) != 1 || la[0].LiveActivity.ContentState.Status != wire.LiveWorking {
		t.Fatalf("got %d pushes within two seconds, want the first only", len(la))
	}
	e.clock = e.clock.Add(time.Second)
	e.tick()
	la, _ = e.livePushes()
	if len(la) != 2 {
		t.Fatalf("got %d pushes after the window, want 2", len(la))
	}
	cs := la[1].LiveActivity.ContentState
	if cs.Status != wire.LiveWorking || *cs.Bib != "03" || cs.Since != e.clock.Add(-1500*time.Millisecond).Unix() {
		t.Fatalf("coalesced state %+v", cs)
	}
	// Needs you is never held back.
	e.clock = e.clock.Add(100 * time.Millisecond)
	e.handle(approvalOpen("r_abcdefghij"))
	if la, _ = e.livePushes(); len(la) != 3 || la[2].LiveActivity.ContentState.Status != wire.LevelNeedsYou {
		t.Fatalf("needs you was coalesced: %d pushes", len(la))
	}
}

// The website promises nothing from the terminal leaves unencrypted.
func TestLiveCommandNeverInPlain(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", updateToken)
	e.pairWith(t, "dev_b", func(p *wire.PairParams) {
		p.LiveActivityToken = strings.Repeat("12", 32)
		p.LiveActivityCommand = true
	})
	ev := withSubject(approvalOpen("r_abcdefghij"), wire.SubjectCommand, "│ ❯ rm  -rf   ./build │\n"+strings.Repeat("x", 300))
	e.handle(ev)
	la, raw := e.livePushes()
	if len(la) != 2 {
		t.Fatalf("got %d live activity pushes", len(la))
	}
	for i, p := range la {
		var body struct {
			LiveActivity struct {
				ContentState map[string]any `json:"contentState"`
			} `json:"liveActivity"`
		}
		if err := json.Unmarshal(raw[i], &body); err != nil {
			t.Fatal(err)
		}
		cmd, present := body.LiveActivity.ContentState["command"]
		switch p.Token {
		case updateToken:
			if !present || cmd != nil {
				t.Fatalf("not opted in: command %v present=%v", cmd, present)
			}
		default:
			// Opted in or not, command text goes only sealed (ctx subject).
			if !present || cmd != nil {
				t.Fatalf("opted in: plain command %v", cmd)
			}
			if c := e.liveCtx(t, p, "dev_b"); c.Subject == nil || !strings.Contains(c.Subject.Text, "rm -rf ./build") {
				t.Fatalf("opted in: sealed subject %+v", c.Subject)
			}
		}
		if bytes.Contains(raw[i], []byte("Run npm test")) {
			t.Fatalf("live activity body carries the title: %s", raw[i])
		}
	}
	for _, b := range raw {
		if bytes.Contains(b, []byte("rm -rf")) {
			t.Fatal("command text left the host in plain")
		}
	}
}

func TestWatchDeviceGetsSilentWatchPushesOnly(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "w_watch", func(p *wire.PairParams) {
		p.Platform = wire.PlatformWatch
		p.LiveActivityToken = updateToken
		p.LiveActivityStartToken = startToken
	})
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(approvalClosed("r_abcdefghij"))
	got := e.relay.received()
	if len(got) != 2 {
		t.Fatalf("relay got %d pushes, want the request and its clear", len(got))
	}
	// The phone's alert is mirrored to the Watch with its actions; the Watch app itself only
	// updates its list and complication, so it never gets an alert of its own.
	for _, p := range got {
		if p.Target != targetWatch || p.Kind != relayBackground || p.LiveActivity != nil {
			t.Fatalf("watch push %+v", p)
		}
	}
}

func TestAlertPlaintextCarriesSubjectAndHost(t *testing.T) {
	e := newEnv(t, 1)
	e.sender.Host = "studio"
	e.handle(withSubject(approvalOpen("r_abcdefghij"), wire.SubjectFile, "~/Проекты/app/main.go"))
	e.handle(notifyEv("s_abcdefghij", wire.LevelDone))
	got := e.relay.received()
	if len(got) != 2 {
		t.Fatalf("relay got %d pushes", len(got))
	}
	p := e.open(t, got[0].Ciphertext, "dev_a")
	if p.Host == nil || *p.Host != "studio" || p.Subject == nil || *p.Subject != (wire.Subject{Kind: wire.SubjectFile, Text: "~/Проекты/app/main.go"}) {
		t.Fatalf("needs_you plaintext host %v subject %v", p.Host, p.Subject)
	}
	if d := e.open(t, got[1].Ciphertext, "dev_a"); d.Host == nil || *d.Host != "studio" || d.Subject != nil {
		t.Fatalf("done plaintext host %v subject %v", d.Host, d.Subject)
	}
}

func TestDefaultHostIsShortHostname(t *testing.T) {
	if got := shortHost("studio.local"); got != "studio" {
		t.Fatalf("shortHost = %q", got)
	}
	if got := shortHost(strings.Repeat("h", 80)); len(got) != wire.MaxHostName {
		t.Fatalf("shortHost kept %d characters", len(got))
	}
}

func TestRelayGoneClearsOnlyThatLiveToken(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", updateToken)
	list, _ := e.svc.Devices().List()
	if err := e.svc.Devices().MarkDead("dev_a", updateToken); err != nil {
		t.Fatal(err)
	}
	after, _ := e.svc.Devices().List()
	if after[0].Dead || after[0].LiveActivityToken != "" || after[0].APNsToken != list[0].APNsToken {
		t.Fatalf("410 on the live activity token: dead=%v", after[0].Dead)
	}
}

func TestUpdateTokensUnknownDevice(t *testing.T) {
	e := newEnv(t, 0)
	if err := e.svc.Devices().UpdateTokens("nobody", strings.Repeat("ab", 32), "", "", false, 1); err != ErrUnknownDevice {
		t.Fatalf("err %v", err)
	}
}

func TestLiveStateCarriesExitReasonOnlyWhenEndedOrFailed(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	tr := &LiveTracker{}
	tr.Session("s_x", wire.LiveWorking, nil, "claude", "", now)
	st, _, ok := tr.Next(now)
	if !ok || st.Status != wire.LiveWorking || st.Reason != "" {
		t.Fatalf("working state %+v ok=%v", st, ok)
	}
	raw, err := json.Marshal(st)
	if err != nil || strings.Contains(string(raw), `"reason"`) {
		t.Fatalf("working JSON %s err=%v", raw, err)
	}

	now = now.Add(time.Second)
	tr.Session("s_x", wire.StatusFailed, nil, "claude", wire.ExitReasonUsageLimit, now)
	st, _, ok = tr.Next(now)
	if !ok || st.Status != wire.StatusFailed || st.Reason != "usage_limit" {
		t.Fatalf("failed state %+v ok=%v", st, ok)
	}
	raw, err = json.Marshal(st)
	if err != nil || !strings.Contains(string(raw), `"reason":"usage_limit"`) {
		t.Fatalf("failed JSON %s err=%v", raw, err)
	}

}

func TestLiveUpdateCarriesExitReasonOnTheWire(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", updateToken)
	e.handle(sessionEv("s_aaaaaaaaaa", "02", "working"))
	e.clock = e.clock.Add(time.Second)
	code := 1
	detail := "secret detail"
	ev := sessionEv("s_aaaaaaaaaa", "02", wire.StatusFailed)
	pl := ev.Payload.(wire.SessionEvent)
	sess := pl.Session.(session.Session)
	sess.Exit = &wire.SessionExit{Code: &code, Reason: wire.ExitReasonUsageLimit, Detail: &detail}
	pl.Session = sess
	ev.Payload = pl
	e.handle(ev)
	e.clock = e.clock.Add(time.Minute)
	e.tick()
	_, raw := e.livePushes()
	last := string(raw[len(raw)-1])
	if !strings.Contains(last, `"reason":"usage_limit"`) || strings.Contains(last, "secret detail") {
		t.Fatalf("failed update body %s", last)
	}
}

func exitedLiveBody(t *testing.T, status string, exit *wire.SessionExit) string {
	t.Helper()
	e := newEnv(t, 0)
	e.pair(t, "dev_a", updateToken)
	e.handle(sessionEv("s_aaaaaaaaaa", "02", "working"))
	e.clock = e.clock.Add(time.Second)
	ev := sessionEv("s_aaaaaaaaaa", "02", status)
	pl := ev.Payload.(wire.SessionEvent)
	sess := pl.Session.(session.Session)
	sess.Exit = exit
	pl.Session = sess
	ev.Payload = pl
	e.handle(ev)
	e.clock = e.clock.Add(time.Minute)
	e.tick()
	_, raw := e.livePushes()
	return string(raw[len(raw)-1])
}

func TestLiveReasonUsesFixedVocabulary(t *testing.T) {
	code := 1
	for _, r := range []string{"exit", "interrupted", "error", "usage_limit"} {
		body := exitedLiveBody(t, wire.StatusFailed, &wire.SessionExit{Code: &code, Reason: r})
		if !strings.Contains(body, `"reason":"`+r+`"`) {
			t.Fatalf("%s missing from %s", r, body)
		}
	}
	// A session exit never carries "restart", so the Live Activity takes it as unknown too.
	for _, r := range []string{"rm -rf secret", "", "Usage_Limit", "brand_new", "restart"} {
		body := exitedLiveBody(t, wire.StatusFailed, &wire.SessionExit{Code: &code, Reason: r})
		if strings.Contains(body, `"reason"`) || strings.Contains(body, "rm -rf") {
			t.Fatalf("reason %q reached the body: %s", r, body)
		}
	}
}

func TestLiveReasonOmittedWithoutExit(t *testing.T) {
	if body := exitedLiveBody(t, wire.StatusEnded, nil); strings.Contains(body, `"reason"`) {
		t.Fatalf("ended without exit carries a reason: %s", body)
	}
}

func TestLiveReasonClearsWhenWorkingAgainOrDone(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	tr := &LiveTracker{}
	tr.Session("s_x", wire.LiveWorking, nil, "claude", "", now)
	tr.Next(now)
	tr.Session("s_x", wire.StatusFailed, nil, "claude", "usage_limit", now.Add(time.Second))
	if st, _, _ := tr.Next(now.Add(time.Second)); st.Reason != "usage_limit" {
		t.Fatalf("failed state %+v", st)
	}
	tr.Session("s_x", wire.LiveWorking, nil, "claude", "", now.Add(2*time.Second))
	if st, _, _ := tr.Next(now.Add(2 * time.Second)); st.Status != wire.LiveWorking || st.Reason != "" {
		t.Fatalf("working again %+v", st)
	}
	tr.Session("s_x", "idle", nil, "claude", "", now.Add(3*time.Second))
	if st, _, _ := tr.Next(now.Add(3 * time.Second)); st.Status != wire.PushDone || st.Reason != "" {
		t.Fatalf("done state %+v", st)
	}
}
