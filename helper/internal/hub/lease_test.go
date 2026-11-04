package hub_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/hub"
	"github.com/gettogo/togo/helper/internal/wire"
)

func (e *env) advance(d time.Duration) { e.elapsed.Add(int64(d)) }

func TestForegroundLapsesWithoutRequests(t *testing.T) {
	e := newEnv(t)
	c := e.dial(t)
	c.subscribe(true)

	e.advance(hub.ForegroundLease - time.Second)
	if n := e.hub.ForegroundSubscribers(); n != 1 {
		t.Fatalf("within the lease: %d foreground, want 1", n)
	}
	e.advance(2 * time.Second)
	if n := e.hub.ForegroundSubscribers(); n != 0 {
		t.Fatalf("after the lease with no requests: %d foreground, want 0", n)
	}
}

func TestAnyRequestRenewsForeground(t *testing.T) {
	e := newEnv(t)
	c := e.dial(t)
	c.subscribe(true)

	e.advance(hub.ForegroundLease + time.Second)
	c.call("session.list", map[string]any{})
	if n := e.hub.ForegroundSubscribers(); n != 1 {
		t.Fatalf("after session.list: %d foreground, want 1", n)
	}
}

func TestKeepaliveRenewsForeground(t *testing.T) {
	e := newEnv(t)
	c := e.dial(t)
	c.subscribe(true)

	e.advance(hub.ForegroundLease + time.Second)
	resp := c.call(wire.MethodEventsKeepalive, map[string]any{})
	if got := string(resp["result"]); got != "{}" {
		t.Fatalf("keepalive result %s %s, want {}", got, resp["error"])
	}
	if n := e.hub.ForegroundSubscribers(); n != 1 {
		t.Fatalf("after keepalive: %d foreground, want 1", n)
	}
	e.advance(hub.ForegroundLease + time.Second)
	if n := e.hub.ForegroundSubscribers(); n != 0 {
		t.Fatalf("lease not renewed again: %d foreground, want 0", n)
	}
}

func TestBackgroundSubscriberStaysBackgroundAfterRequests(t *testing.T) {
	e := newEnv(t)
	c := e.dial(t)
	c.subscribe(false)
	c.call(wire.MethodEventsKeepalive, json.RawMessage(`{}`))
	if n := e.hub.ForegroundSubscribers(); n != 0 {
		t.Fatalf("background subscriber counted as foreground: %d", n)
	}
}
