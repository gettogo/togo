package push

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestShortSubjectKeepsMeaning(t *testing.T) {
	long := "npm run build -- --filter @togo/web --mode production && npm test -- --reporter dot --bail src/very/long/spec.ts"
	for _, c := range []struct {
		name string
		in   wire.Subject
		n    int
		want string
	}{
		{"short command untouched", wire.Subject{Kind: wire.SubjectCommand, Text: "npm test"}, 40, "npm test"},
		{"command keeps start and end", wire.Subject{Kind: wire.SubjectCommand, Text: long}, 40, "npm run build -- --filt…ery/long/spec.ts"},
		{"path keeps file name", wire.Subject{Kind: wire.SubjectFile, Text: "/Users/jamie/Dev/togo/helper/internal/push/live.go"}, 30, "~/…/internal/push/live.go"},
		{"home becomes tilde", wire.Subject{Kind: wire.SubjectFile, Text: "/home/ana/src/a.go"}, 40, "~/src/a.go"},
		{"relative path", wire.Subject{Kind: wire.SubjectFile, Text: "apps/ios/TogoWidgets/LiveLockScreenView.swift"}, 34, "apps/…/LiveLockScreenView.swift"},
		{"text loses its end", wire.Subject{Kind: wire.SubjectText, Text: "Which database should the new service use for sessions?"}, 20, "Which database shou…"},
		{"url loses its middle", wire.Subject{Kind: wire.SubjectURL, Text: "https://example.com/a/very/long/path/to/some/page?x=1"}, 30, "https://example.c…ome/page?x=1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := shortSubject(c.in, c.n)
			if got.Text != c.want {
				t.Fatalf("got %q, want %q", got.Text, c.want)
			}
			if n := utf8.RuneCountInString(got.Text); n > c.n {
				t.Fatalf("%d runes, limit %d", n, c.n)
			}
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestShortSubjectCommandFlattensLines(t *testing.T) {
	got := shortSubject(wire.Subject{Kind: wire.SubjectCommand, Text: "cat <<EOF\nhello\nEOF"}, 100)
	if strings.Contains(got.Text, "\n") || got.Text != "cat <<EOF hello EOF" {
		t.Fatalf("got %q", got.Text)
	}
}

func TestSealLiveContextFitsAndOpens(t *testing.T) {
	priv, pub, err := seal.GenerateX25519()
	if err != nil {
		t.Fatal(err)
	}
	c := LiveContext{Title: strings.Repeat("題", maxLiveTitle), Step: strings.Repeat("s", maxLiveStep),
		Ask: strings.Repeat("問", maxLiveAsk), Answer: liveAnswerApproveDeny,
		Subject: &wire.Subject{Kind: wire.SubjectCommand, Text: strings.Repeat("<&>界", maxLiveSubject/4)}}
	aad := []byte("h_x|d_y")
	ct := sealLiveContext(pub, c, aad)
	if ct == "" || len(ct) > maxLiveCtx {
		t.Fatalf("envelope %d chars, limit %d", len(ct), maxLiveCtx)
	}
	if _, err := seal.Open(priv, ct, seal.LabelPush, aad); err == nil {
		t.Fatal("live context opened as a push")
	}
	raw, err := seal.Open(priv, ct, seal.LabelLive, aad)
	if err != nil {
		t.Fatal(err)
	}
	var got LiveContext
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Step != "" || got.Subject == nil || got.Ask == "" || got.Answer != liveAnswerApproveDeny {
		t.Fatalf("shrunk in the wrong order: %+v", got)
	}
	if err := got.Subject.Validate(); err != nil {
		t.Fatal(err)
	}
	if sealLiveContext(pub, LiveContext{}, aad) != "" {
		t.Fatal("empty context sealed")
	}
}

// liveCtx opens the sealed context of a Live Activity push for a phone.
func (e *env) liveCtx(t *testing.T, la relayPush, deviceID string) LiveContext {
	t.Helper()
	if la.LiveActivity.ContentState.Ctx == nil {
		t.Fatal("no ctx")
	}
	keys, _ := e.svc.HostKeys()
	for _, ph := range e.phones {
		if ph.id != deviceID {
			continue
		}
		raw, err := seal.Open(ph.priv, *la.LiveActivity.ContentState.Ctx, seal.LabelLive, []byte(keys.HostID+"|"+deviceID))
		if err != nil {
			t.Fatal(err)
		}
		var c LiveContext
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	t.Fatalf("no phone %s", deviceID)
	return LiveContext{}
}

func namedSessions(e *env, name string) {
	e.sender.Session = func(id string) (SessionInfo, bool) {
		bib, agent := "01", "claude"
		return SessionInfo{Bib: &bib, Agent: &agent, Muted: e.muted[id], Name: name}, true
	}
}

func TestLiveActivityCarriesSealedRequestContext(t *testing.T) {
	e := newEnv(t, 0)
	namedSessions(e, "Fix the login redirect")
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(withSubject(approvalOpen("r_abcdefghij"), wire.SubjectCommand, "npm test -- --watch=false"))
	la, raw := e.livePushes()
	if len(la) != 1 {
		t.Fatalf("got %d live pushes", len(la))
	}
	// Command text stays sealed for a device that did not opt in.
	if bytes.Contains(raw[0], []byte("npm test")) || bytes.Contains(raw[0], []byte("login")) {
		t.Fatalf("plain context on the wire: %s", raw[0])
	}
	c := e.liveCtx(t, la[0], "dev_a")
	if c.Title != "Fix the login redirect" || c.Ask != "Run npm test?" || c.Answer != liveAnswerApproveDeny ||
		c.Subject == nil || c.Subject.Text != "npm test -- --watch=false" || c.Step != "" {
		t.Fatalf("context %+v", c)
	}
	if len(raw[0]) > 4096-512 {
		t.Fatalf("relay body %d bytes leaves no room for the aps wrapper", len(raw[0]))
	}
}

func TestLivePushFitsWithLongestContext(t *testing.T) {
	e := newEnv(t, 0)
	namedSessions(e, strings.Repeat("名", 200))
	e.pairWith(t, "dev_a", func(p *wire.PairParams) {
		p.LiveActivityStartToken = startToken
		p.LiveActivityCommand = true
	})
	ev := approvalOpen("r_abcdefghij")
	a := ev.Payload.(wire.ApprovalOpenEvent)
	a.Approval.Title = strings.Repeat("問", wire.MaxTitle)
	a.Approval.Subject = &wire.Subject{Kind: wire.SubjectCommand, Text: strings.Repeat("\"<界>\"", wire.MaxSubjectText/5)}
	ev.Payload = a
	e.handle(ev)
	la, raw := e.livePushes()
	if len(la) != 1 || la[0].LiveActivity.ContentState.Ctx == nil {
		t.Fatalf("got %d live pushes", len(la))
	}
	// APNs takes 4 KB; the relay adds the aps keys around the content state.
	if len(raw[0]) > 3600 {
		t.Fatalf("relay body %d bytes", len(raw[0]))
	}
	if c := e.liveCtx(t, la[0], "dev_a"); c.Subject == nil || c.Ask == "" {
		t.Fatalf("context lost its request: %+v", c)
	}
}

func TestLiveQuestionThatCannotBeAnsweredOffersNoButtons(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.pair(t, "dev_b", "")
	ev := approvalOpen("r_abcdefghij")
	a := ev.Payload.(wire.ApprovalOpenEvent)
	no := false
	a.Approval.Answerable = &no
	a.Approval.Title = "Which color do you prefer?"
	ev.Payload = a
	e.handle(ev)
	la, _ := e.livePushes()
	if c := e.liveCtx(t, la[0], "dev_a"); c.Answer != "" || c.Ask != "Which color do you prefer?" {
		t.Fatalf("context %+v", c)
	}
	keys, _ := e.svc.HostKeys()
	opened := 0
	for _, p := range e.relay.received() {
		if p.Kind != relayAlert {
			continue
		}
		raw, err := openRaw(e.phones[1], p.Ciphertext, keys.HostID)
		if err != nil {
			continue
		}
		opened++
		var pt wire.PushPlaintext
		if err := json.Unmarshal(raw, &pt); err != nil || len(pt.Options) != 0 || pt.AllowText {
			t.Fatalf("alert offers answers that cannot work: %s", raw)
		}
	}
	if opened != 1 {
		t.Fatalf("dev_b opened %d alerts", opened)
	}
}

func TestLiveWorkingShowsTitleAndStepAndHoldsStepChanges(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityToken = updateToken })
	working := func(step string) wire.Event {
		ev := sessionEv("s_aaaaaaaaaa", "02", wire.LiveWorking)
		se := ev.Payload.(wire.SessionEvent)
		s := se.Session.(session.Session)
		s.Name, s.Activity = "Refactor the parser", &step
		se.Session = s
		ev.Payload = se
		return ev
	}
	e.handle(working("Bash"))
	la, _ := e.livePushes()
	if len(la) != 1 {
		t.Fatalf("got %d pushes", len(la))
	}
	if c := e.liveCtx(t, la[0], "dev_a"); c.Title != "Refactor the parser" || c.Step != "Bash" {
		t.Fatalf("context %+v", c)
	}
	e.clock = e.clock.Add(3 * time.Second)
	e.handle(working("Edit"))
	if la, _ := e.livePushes(); len(la) != 1 {
		t.Fatalf("step change sent after 3 s: %d pushes", len(la))
	}
	e.clock = e.clock.Add(7 * time.Second)
	e.tick()
	la, _ = e.livePushes()
	if len(la) != 2 {
		t.Fatalf("held step change not sent after 10 s: %d pushes", len(la))
	}
	if c := e.liveCtx(t, la[1], "dev_a"); c.Step != "Edit" {
		t.Fatalf("context %+v", c)
	}
}

func TestMutedSessionNeverTouchesLiveActivity(t *testing.T) {
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.muted["s_abcdefghij"] = true
	e.handle(approvalOpen("r_abcdefghij"))
	if got := e.relay.received(); len(got) != 0 {
		t.Fatalf("muted session sent %d pushes", len(got))
	}
	ev := sessionEv("s_abcdefghij", "01", wire.LiveWorking)
	se := ev.Payload.(wire.SessionEvent)
	s := se.Session.(session.Session)
	s.Muted = true
	se.Session = s
	ev.Payload = se
	e.handle(ev)
	e.tick()
	if got := e.relay.received(); len(got) != 0 {
		t.Fatalf("muted working session sent %d pushes", len(got))
	}
}

func TestMutingASessionDropsItsWaitingRequest(t *testing.T) {
	tr := &LiveTracker{}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	tr.OpenRequest("r_aaaaaaaaaa", now, sid("01"), "claude", nil, "s_one", LiveContext{Ask: "One?"}, now)
	tr.OpenRequest("r_bbbbbbbbbb", now.Add(time.Second), sid("02"), "codex", nil, "s_two", LiveContext{Ask: "Two?"}, now)
	if st, _, _ := tr.Next(now); st.Waiting != 2 || st.context.Ask != "One?" {
		t.Fatalf("state %+v", st)
	}
	tr.Remove("s_one", now)
	st, _, ok := tr.Next(now)
	if !ok || st.Waiting != 1 || *st.RequestID != "r_bbbbbbbbbb" || st.context.Ask != "Two?" {
		t.Fatalf("after mute %+v ok=%v", st, ok)
	}
}

func TestMachineMuteKeepsLiveActivityQuiet(t *testing.T) {
	e := newEnv(t, 0)
	e.sender.Muted = func() bool { return true }
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	e.handle(approvalOpen("r_abcdefghij"))
	e.handle(sessionEv("s_aaaaaaaaaa", "02", wire.LiveWorking))
	e.tick()
	if got := e.relay.received(); len(got) != 0 {
		t.Fatalf("muted machine sent %d pushes", len(got))
	}
}

func TestAlertCarriesSessionName(t *testing.T) {
	e := newEnv(t, 1)
	namedSessions(e, "Fix the login redirect")
	e.handle(approvalOpen("r_abcdefghij"))
	got := e.relay.received()
	if len(got) != 1 {
		t.Fatalf("got %d pushes", len(got))
	}
	if p := e.open(t, got[0].Ciphertext, "dev_a"); p.SessionName == nil || *p.SessionName != "Fix the login redirect" {
		t.Fatalf("session name %v", p.SessionName)
	}
}

func TestShortPathTempAndHome(t *testing.T) {
	for in, want := range map[string]string{
		"/private/tmp/workdir-01/-Users-jamie-Dev-Delta-RE/54d95478/scratchpad/harness-e2e/kimi": "…/harness-e2e/kimi",
		"/var/folders/xy/abc/T/build/out.log":                                                    "…/build/out.log",
		"/tmp/a":                                                                                 "/tmp/a",
		"/Users/jamie/Dev/togo/README.md":                                                        "~/Dev/togo/README.md",
	} {
		if got := shortPath(in, 120); got != want {
			t.Errorf("shortPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAlertShortensPathAndNamesCustomProgramAgent(t *testing.T) {
	e := newEnv(t, 1)
	e.sender.Session = func(string) (SessionInfo, bool) {
		bib, agent := "05", "kimi"
		return SessionInfo{Bib: &bib, Agent: &agent, Name: "kimi"}, true
	}
	ev := approvalOpen("r_abcdefghij")
	a := ev.Payload.(wire.ApprovalOpenEvent)
	a.Approval.Agent = wire.AgentCustom
	a.Approval.Title = "Trust this folder?"
	hint := "↑↓ navigate · Enter select · Esc exit\n/private/tmp/workdir-01/x/scratchpad/harness-e2e/kimi"
	a.Approval.Detail = &hint
	a.Approval.Subject = &wire.Subject{Kind: wire.SubjectFile, Text: "/private/tmp/workdir-01/x/scratchpad/harness-e2e/kimi"}
	ev.Payload = a
	e.handle(ev)
	got := e.relay.received()
	p := e.open(t, got[0].Ciphertext, "dev_a")
	if *p.Agent != "kimi" || p.Subject.Text != "…/harness-e2e/kimi" || strings.Contains(p.Body, "navigate") {
		t.Fatalf("agent %q subject %q body %q", *p.Agent, p.Subject.Text, p.Body)
	}
}
