package mailbox

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeWill is a WillSource whose frame the test sets.
type fakeWill struct {
	mu      sync.Mutex
	frame   []byte
	ok      bool
	changed chan struct{}
}

func newFakeWill() *fakeWill { return &fakeWill{changed: make(chan struct{})} }

func (w *fakeWill) Will(string) ([]byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frame, w.ok
}

func (w *fakeWill) Changed() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.changed
}

func (w *fakeWill) set(frame string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.frame, w.ok = []byte(frame), true
	close(w.changed)
	w.changed = make(chan struct{})
}

const armed = `{"type":"will","v":1,"afterMs":300000,"pushes":[{"token":"ab","env":"sandbox","ciphertext":"x","kind":"alert"}]}`

func newWillHarness(t *testing.T, will *fakeWill, marker string) *harness {
	t.Helper()
	return newHarness(t, func(h *harness) {
		h.config = func(c *Config) { c.Will, c.StopMarker = will, marker }
	})
}

func (h *harness) nextWill(t *testing.T) string {
	t.Helper()
	select {
	case f := <-h.relay.wills:
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no will frame")
		return ""
	}
}

func (h *harness) noWill(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case f := <-h.relay.wills:
		t.Fatalf("unexpected will frame %s", f)
	case <-time.After(wait):
	}
}

// stop cancels Run and waits for it, leaving the result for the cleanup.
func (h *harness) stop(t *testing.T) {
	t.Helper()
	h.cancel()
	select {
	case err := <-h.done:
		h.done <- err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestWillSentOnConnect(t *testing.T) {
	w := newFakeWill()
	w.set(armed)
	h := newWillHarness(t, w, "")
	if got := h.nextWill(t); got != armed {
		t.Fatalf("frame %s", got)
	}
	// A reconnect sends it again.
	before := h.relay.dialCount()
	h.relay.mu.Lock()
	h.relay.conn.CloseNow()
	h.relay.mu.Unlock()
	if got := h.nextWill(t); got != armed {
		t.Fatalf("frame after reconnect %s", got)
	}
	if h.relay.dialCount() <= before {
		t.Fatal("no reconnect")
	}
}

func TestNoWillBeforeFirstBuild(t *testing.T) {
	w := newFakeWill()
	h := newWillHarness(t, w, "")
	<-h.relay.conned
	h.noWill(t, 100*time.Millisecond)
	w.set(armed)
	if got := h.nextWill(t); got != armed {
		t.Fatalf("frame %s", got)
	}
}

func TestWillResentOnChange(t *testing.T) {
	w := newFakeWill()
	w.set(armed)
	h := newWillHarness(t, w, "")
	h.nextWill(t)
	w.set(DisarmFrame)
	if got := h.nextWill(t); got != DisarmFrame {
		t.Fatalf("frame %s", got)
	}
	// The same frame again is not resent.
	w.set(DisarmFrame)
	h.noWill(t, 100*time.Millisecond)
}

func TestStopMarkerDisarms(t *testing.T) {
	marker := filepath.Join(t.TempDir(), StopMarkerName)
	w := newFakeWill()
	w.set(armed)
	h := newWillHarness(t, w, marker)
	h.nextWill(t)
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h.stop(t)
	if got := h.nextWill(t); got != DisarmFrame {
		t.Fatalf("frame on stop %s, want the disarm frame", got)
	}
}

func TestSigtermKeepsWill(t *testing.T) {
	marker := filepath.Join(t.TempDir(), StopMarkerName)
	w := newFakeWill()
	w.set(armed)
	h := newWillHarness(t, w, marker)
	h.nextWill(t)
	h.stop(t)
	h.noWill(t, 200*time.Millisecond)
}

func TestStaleStopMarkerClearedAtStart(t *testing.T) {
	marker := filepath.Join(t.TempDir(), StopMarkerName)
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	w := newFakeWill()
	w.set(armed)
	h := newWillHarness(t, w, marker)
	h.nextWill(t)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker still there: %v", err)
	}
	h.stop(t)
	h.noWill(t, 200*time.Millisecond)
}

func TestDroppedRelayDisarms(t *testing.T) {
	w := newFakeWill()
	w.set(armed)
	h := newWillHarness(t, w, "")
	h.nextWill(t)
	h.setPaired(false)
	if got := h.nextWill(t); got != DisarmFrame {
		t.Fatalf("frame when the relay was dropped %s", got)
	}
}

func TestReaddedRelayWaitsForOldDisarm(t *testing.T) {
	w := newFakeWill()
	w.set(armed)
	entered := make(chan struct{}, 1)
	h := newHarness(t, func(h *harness) {
		h.config = func(c *Config) { c.Will = w }
		h.runner = func(r *runner) {
			r.beforeDisarm = func() {
				entered <- struct{}{}
				time.Sleep(300 * time.Millisecond)
			}
		}
	})
	h.nextWill(t)
	h.setPaired(false)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("dropped relay never disarmed")
	}
	h.setPaired(true) // wanted again while the old connection is disarming
	if got := h.nextWill(t); got != DisarmFrame {
		t.Fatalf("first frame after the drop %s, want the disarm", got)
	}
	if got := h.nextWill(t); got != armed {
		t.Fatalf("frame on the new connection %s", got)
	}
	h.relay.mu.Lock()
	at := append([]int(nil), h.relay.willAt...)
	h.relay.mu.Unlock()
	if len(at) != 3 || at[1] != 1 || at[2] != 2 {
		t.Fatalf("dials when each will arrived %v; the disarm must land before the new connection", at)
	}
	h.noWill(t, 100*time.Millisecond)
}
