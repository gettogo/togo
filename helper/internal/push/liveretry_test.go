package push

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// An update the relay did not take is sent again, with a growing wait, until it lands.
func TestLiveUpdateRetriedAfterRelayFailure(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", updateToken)
	e.handle(sessionEv("s_aaaaaaaaaa", "02", "working"))
	if la, _ := e.livePushes(); len(la) != 1 {
		t.Fatalf("got %d live pushes, want the first update", len(la))
	}
	e.relay.status.Store(http.StatusBadGateway)
	e.clock = e.clock.Add(time.Minute)
	e.handle(sessionEv("s_aaaaaaaaaa", "02", "idle"))
	failed, _ := e.livePushes()
	if len(failed) < 2 || failed[len(failed)-1].LiveActivity.ContentState.Status != "done" {
		t.Fatalf("done update not attempted: %d pushes", len(failed))
	}
	e.relay.status.Store(http.StatusAccepted)
	e.clock = e.clock.Add(liveCoalesce)
	e.tick()
	if la, _ := e.livePushes(); len(la) != len(failed) {
		t.Fatalf("retried before its wait: %d pushes, had %d", len(la), len(failed))
	}
	e.clock = e.clock.Add(liveRetryWait(1))
	e.tick()
	la, _ := e.livePushes()
	if len(la) != len(failed)+1 || la[len(la)-1].LiveActivity.ContentState.Status != "done" {
		t.Fatalf("done update not sent again: %d pushes, had %d", len(la), len(failed))
	}
	e.clock = e.clock.Add(5 * time.Minute)
	e.tick()
	if again, _ := e.livePushes(); len(again) != len(la) {
		t.Fatalf("delivered update sent again: %d pushes, had %d", len(again), len(la))
	}
}

// A relay that stays down is not asked forever.
func TestLiveUpdateRetryIsBounded(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", updateToken)
	e.handle(sessionEv("s_aaaaaaaaaa", "02", "working"))
	e.relay.status.Store(http.StatusBadGateway)
	e.clock = e.clock.Add(time.Minute)
	e.handle(sessionEv("s_aaaaaaaaaa", "02", "idle"))
	// Inside liveQuiet, so the end of the activity is not counted.
	for range 18 {
		e.clock = e.clock.Add(30 * time.Second)
		e.tick()
	}
	if !strings.Contains(e.logs.String(), "given up") {
		t.Fatalf("retries not given up: %s", e.logs.String())
	}
	stopped, _ := e.livePushes()
	for range 2 {
		e.clock = e.clock.Add(10 * time.Second)
		e.tick()
	}
	if after, _ := e.livePushes(); len(after) != len(stopped) {
		t.Fatalf("still retrying after giving up: %d pushes, had %d", len(after), len(stopped))
	}
}

// After the relay told the devices "offline", the current state goes out again.
func TestResendLiveAfterRelaySilence(t *testing.T) {
	e := newEnv(t, 0)
	e.pair(t, "dev_a", updateToken)
	e.handle(sessionEv("s_aaaaaaaaaa", "02", "working"))
	first, _ := e.livePushes()
	e.clock = e.clock.Add(6 * time.Minute)
	e.tick()
	if la, _ := e.livePushes(); len(la) != len(first) {
		t.Fatalf("unchanged state sent without a reason: %d pushes", len(la))
	}
	e.sender.ResendLive()
	e.tick()
	la, _ := e.livePushes()
	if len(la) != len(first)+1 || la[len(la)-1].LiveActivity.ContentState.Status != "working" {
		t.Fatalf("current state not sent again: %d pushes, had %d", len(la), len(first))
	}
}
