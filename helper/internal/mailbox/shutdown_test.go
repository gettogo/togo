package mailbox

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func (r *fakeRelay) recordWill(data string) {
	if r.beforeWill != nil {
		r.beforeWill(data)
	}
	r.mu.Lock()
	r.willAt = append(r.willAt, len(r.dials))
	r.mu.Unlock()
	r.wills <- data
}

func TestCanceledControlWritePreservesFinalDisarm(t *testing.T) {
	w := newFakeWill()
	w.set(armed)
	connected := make(chan *websocket.Conn, 1)
	h := newHarness(t, func(h *harness) {
		h.config = func(c *Config) {
			c.Will = w
			c.Dial = func(ctx context.Context, url string, opts *websocket.DialOptions) (*websocket.Conn, *http.Response, error) {
				conn, resp, err := websocket.Dial(ctx, url, opts)
				if err == nil {
					connected <- conn
				}
				return conn, resp, err
			}
		}
	})
	h.nextWill(t)
	conn := <-connected
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := writeControl(ctx, conn, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("session cancellation closed control write: %v", err)
	}
	h.setPaired(false)
	if got := h.nextWill(t); got != DisarmFrame {
		t.Fatalf("final frame %s", got)
	}
}

func TestUnresponsiveDisarmStillBoundsShutdown(t *testing.T) {
	w := newFakeWill()
	w.set(armed)
	entered, release := make(chan struct{}), make(chan struct{})
	h := newHarness(t, func(h *harness) {
		h.config = func(c *Config) { c.Will = w }
		h.relay.beforeWill = func(data string) {
			if data == DisarmFrame {
				close(entered)
				<-release
			}
		}
	})
	defer close(release)
	h.nextWill(t)
	h.setPaired(false)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("relay did not receive disarm")
	}
	h.stop(t)
	if !strings.Contains(h.logs.String(), "disarm transport did not drain") {
		t.Fatalf("missing forced-close failure feedback: %s", h.logs.String())
	}
}

func TestReplacementWaitsForRelayToReadDisarm(t *testing.T) {
	w := newFakeWill()
	w.set(armed)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h := newHarness(t, func(h *harness) {
		h.config = func(c *Config) { c.Will = w }
		h.relay.beforeWill = func(data string) {
			if data == DisarmFrame {
				close(entered)
				<-release
			}
		}
	})
	defer once.Do(func() { close(release) })
	h.nextWill(t)
	<-h.relay.conned
	h.setPaired(false)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("relay did not receive disarm")
	}
	h.setPaired(true)
	select {
	case <-h.relay.conned:
		t.Fatal("replacement connected before the relay handled the old disarm")
	case <-time.After(150 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	if got := h.nextWill(t); got != DisarmFrame {
		t.Fatalf("old frame %s, want disarm", got)
	}
	if got := h.nextWill(t); got != armed {
		t.Fatalf("new frame %s, want armed", got)
	}
}

func TestRepeatedPairingKeepsOriginalShutdownBarrier(t *testing.T) {
	w := newFakeWill()
	w.set(armed)
	entered, release := make(chan struct{}), make(chan struct{})
	scans := make(chan bool, 32)
	var once sync.Once
	h := newHarness(t, func(h *harness) {
		h.config = func(c *Config) {
			c.Will = w
			c.Devices = func() []Device {
				devs := h.devices()
				select {
				case scans <- len(devs) > 0:
				default:
				}
				return devs
			}
		}
		h.runner = func(r *runner) {
			r.beforeDisarm = func() { close(entered); <-release }
		}
	})
	defer once.Do(func() { close(release) })
	h.nextWill(t)
	<-h.relay.conned
	h.setPaired(false)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("old worker did not stop")
	}
	setAndObserve := func(paired bool) {
		for len(scans) > 0 {
			<-scans
		}
		h.setPaired(paired)
		for seen := 0; seen < 2; {
			select {
			case got := <-scans:
				if got == paired {
					seen++
				}
			case <-time.After(5 * time.Second):
				t.Fatal("supervisor did not rescan")
			}
		}
	}
	setAndObserve(true)
	setAndObserve(false)
	setAndObserve(true)
	select {
	case <-h.relay.conned:
		t.Fatal("canceled replacement bypassed original shutdown")
	case <-time.After(150 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	if got := h.nextWill(t); got != DisarmFrame {
		t.Fatalf("old frame %s", got)
	}
	if got := h.nextWill(t); got != armed {
		t.Fatalf("new frame %s", got)
	}
}
