package push

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

func exitEv(id, status string, exit *wire.SessionExit) wire.Event {
	return wire.Event{Kind: wire.EventSession, SessionID: sid(id), Payload: wire.SessionEvent{Session: session.Session{
		ID: id, Bib: sid("01"), Name: "Claude Code", Agent: "claude", Status: status, Managed: true, Exit: exit,
	}}}
}

func code(n int) *int { return &n }

// keysOf lists an object's keys and the JSON type of each value.
func keysOf(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for k, v := range m {
		out[k] = reflect.TypeOf(v).String()
	}
	return out
}

// TestEndedAndFailedPushes checks the plaintext of each against the shape
// of the shared examples: the same keys with the same JSON types, the
// exit as sent, the session name as title and an empty body.
func TestEndedAndFailedPushes(t *testing.T) {
	limit := "Usage limit reached"
	for _, c := range []struct {
		fixture, status string
		exit            wire.SessionExit
	}{
		{"push.ended.json", wire.StatusEnded, wire.SessionExit{Code: code(0), Reason: wire.ExitReasonExit}},
		{"push.failed.json", wire.StatusFailed, wire.SessionExit{Reason: wire.ExitReasonUsageLimit, Detail: &limit}},
	} {
		t.Run(c.fixture, func(t *testing.T) {
			raw, err := os.ReadFile("../../../protocol/v1/fixtures/plaintext/" + c.fixture)
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
			e.sender.Host = "studio"
			e.handle(exitEv("s_abcdefghij", c.status, &c.exit))
			got := e.relay.received()
			if len(got) != 1 || got[0].Kind != relayAlert {
				t.Fatalf("relay got %+v, want one alert", got)
			}
			keys, _ := e.svc.HostKeys()
			plain, err := openRaw(e.phones[0], got[0].Ciphertext, keys.HostID)
			if err != nil {
				t.Fatal(err)
			}
			if g, w := keysOf(t, plain), keysOf(t, fx.Plaintext); !reflect.DeepEqual(g, w) {
				t.Fatalf("plaintext keys %v, want %v\n%s", sorted(g), sorted(w), plain)
			}
			p := e.open(t, got[0].Ciphertext, e.phones[0].id)
			if p.Kind != c.status || p.Title != "Claude Code" || p.Body != "" || p.Exit == nil || !reflect.DeepEqual(*p.Exit, c.exit) {
				t.Fatalf("plaintext %s", plain)
			}
			if *p.Bib != "01" || *p.Agent != "claude" || *p.SessionID != "s_abcdefghij" {
				t.Fatalf("session fields %s", plain)
			}
		})
	}
}

func sorted(m map[string]string) []string {
	var out []string
	for k, v := range m {
		out = append(out, k+":"+v)
	}
	sort.Strings(out)
	return out
}

// TestExitPushedOnceAndRateLimited sends one push per exit, none for the
// same exit published again, and holds a second exit in the same session
// within 5 s like a done.
func TestExitPushedOnceAndRateLimited(t *testing.T) {
	e := newEnv(t, 1)
	ended := wire.SessionExit{Code: code(0), Reason: wire.ExitReasonExit}
	e.handle(exitEv("s_abcdefghij", wire.StatusEnded, &ended))
	e.clock = e.clock.Add(10 * time.Second)
	e.handle(exitEv("s_abcdefghij", wire.StatusEnded, &ended))
	if n := len(e.relay.received()); n != 1 {
		t.Fatalf("%d pushes for one exit", n)
	}
	e.handle(exitEv("s_abcdefghij", wire.StatusFailed, &wire.SessionExit{Code: code(1), Reason: wire.ExitReasonExit}))
	if n := len(e.relay.received()); n != 2 {
		t.Fatalf("%d pushes after a new exit, want 2", n)
	}
	e.clock = e.clock.Add(time.Second)
	e.handle(sessionEv("s_abcdefghij", "01", "idle"))
	e.handle(exitEv("s_abcdefghij", wire.StatusFailed, &wire.SessionExit{Code: code(2), Reason: wire.ExitReasonExit}))
	if n := len(e.relay.received()); n != 2 {
		t.Fatalf("%d pushes, want the exit within 5 s held back", n)
	}
	e.handle(sessionEv("s_abcdefghij", "01", "working"))
	if n := len(e.relay.received()); n != 2 {
		t.Fatalf("a working session pushed")
	}
}

func TestExitNotPushedWhenMuted(t *testing.T) {
	e := newEnv(t, 1)
	e.muted["s_abcdefghij"] = true
	ev := exitEv("s_abcdefghij", wire.StatusEnded, &wire.SessionExit{Code: code(0), Reason: wire.ExitReasonExit})
	e.handle(ev)
	if n := len(e.relay.received()); n != 0 {
		t.Fatalf("%d pushes for a muted session", n)
	}
}

// TestLiveShowsExitInFocusSlot: the working session that held the focus
// exits, and the activity shows it ended or failed instead of done.
func TestLiveShowsExitInFocusSlot(t *testing.T) {
	for _, status := range []string{wire.StatusEnded, wire.StatusFailed} {
		t.Run(status, func(t *testing.T) {
			e := newEnv(t, 0)
			e.pair(t, "dev_a", updateToken)
			e.handle(sessionEv("s_abcdefghij", "01", wire.LiveWorking))
			e.clock = e.clock.Add(5 * time.Second)
			e.handle(exitEv("s_abcdefghij", status, &wire.SessionExit{Code: code(1), Reason: wire.ExitReasonExit}))
			e.clock = e.clock.Add(5 * time.Second)
			e.tick()
			la, _ := e.livePushes()
			if len(la) == 0 {
				t.Fatal("no Live Activity push")
			}
			if cs := la[len(la)-1].LiveActivity.ContentState; cs.Status != status || cs.Bib == nil || *cs.Bib != "01" {
				t.Fatalf("content state %+v, want %s for 01", cs, status)
			}
			e.handle(sessionEv("s_abcdefghij", "01", wire.LiveWorking))
			e.clock = e.clock.Add(5 * time.Second)
			e.handle(sessionEv("s_abcdefghij", "01", "idle"))
			e.clock = e.clock.Add(5 * time.Second)
			e.tick()
			la, _ = e.livePushes()
			if cs := la[len(la)-1].LiveActivity.ContentState; cs.Status != wire.PushDone {
				t.Fatalf("after the agent ran again: %+v, want done", cs)
			}
		})
	}
}

// An exit the hub restored after a restart was announced before, or
// happened while nothing watched: it sends no push, and the same exit seen
// later as new does not either.
func TestRestoredExitNotPushed(t *testing.T) {
	e := newEnv(t, 1)
	ended := wire.SessionExit{Code: code(0), Reason: wire.ExitReasonExit}
	ev := exitEv("s_abcdefghij", wire.StatusEnded, &ended)
	restored := ev.Payload.(wire.SessionEvent)
	s := restored.Session.(session.Session)
	s.ExitRestored = true
	ev.Payload = wire.SessionEvent{Session: s}
	e.handle(ev)
	e.clock = e.clock.Add(10 * time.Second)
	e.handle(exitEv("s_abcdefghij", wire.StatusEnded, &ended))
	if n := len(e.relay.received()); n != 0 {
		t.Fatalf("%d pushes for a restored exit", n)
	}
}
