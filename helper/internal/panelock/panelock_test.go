package panelock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAcquireWaitsAndGivesUpWithItsRequest(t *testing.T) {
	const id = "s_lockwaitxx"
	held, release, err := Acquire(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	// Nested on the holder's context: taken already, nothing to wait for.
	if _, inner, err := Acquire(held, id); err != nil {
		t.Fatal(err)
	} else {
		inner()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := Acquire(ctx, id); !errors.Is(err, ErrBusy) {
		t.Fatalf("a request that ended while waiting: %v, want ErrBusy", err)
	}
	if _, _, ok := TryAcquire(context.Background(), id); ok {
		t.Fatal("TryAcquire took a held pane")
	}
	release()
	release() // twice is harmless
	_, again, err := Acquire(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestStopEndsTheSequencesUnderWay(t *testing.T) {
	const id = "s_lockstopxx"
	held, release, _ := Acquire(context.Background(), id)
	defer release()
	if Stopped(held, id, 0) {
		t.Fatal("stopped before any stop")
	}
	Stop(id)
	if !Stopped(held, id, 0) {
		t.Fatal("a stop did not reach the sequence holding the pane")
	}
	later, rel2, ok := TryAcquire(context.Background(), "s_lockstopyy")
	if !ok || Stopped(later, "s_lockstopyy", 0) {
		t.Fatal("another pane is stopped")
	}
	rel2()
}

// Forget never replaces a lock someone holds or waits for.
func TestForgetKeepsAHeldPane(t *testing.T) {
	const id = "s_lockgonexx"
	p := For(id)
	p.Lock()
	Forget(id)
	if For(id) != p {
		t.Fatal("a held pane was replaced")
	}
	p.Unlock()
	Forget(id)
	if Held(id) {
		t.Fatal("a free pane of a gone session was kept")
	}
}
