package source

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A hook the helper accepted (`togo hook` got its reply once Deliver
// returned) may still wait in a feed's queue. Drain waits until every
// queued payload has been handed to its feed and the feed is done with
// it, so a helper that exits after Drain has stored what it accepted.
func TestHookBusDrainWaitsForQueuedPayloads(t *testing.T) {
	env, _ := testEnv(t)
	bus := NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	var done []string
	emit := func(r harness.Record) {
		if len(done) == 0 {
			close(started)
			<-release // the feed is busy with the first payload
		}
		done = append(done, string(r.Data))
	}
	watching := make(chan struct{})
	go func() {
		close(watching)
		_ = NewHook("hook", bus).Watch(ctx, sspec("hooks", map[string]any{}), env, emit)
	}()
	<-watching
	for !bus.subscribed(env.SessionID) {
		time.Sleep(time.Millisecond)
	}
	bus.Deliver(env.SessionID, Payload{Kind: BusHook, Event: "PostToolUse", Data: json.RawMessage(`{"n":1}`)})
	bus.Deliver(env.SessionID, Payload{Kind: BusHook, Event: "Stop", Data: json.RawMessage(`{"n":2}`)})
	<-started
	if bus.Drain("", 50*time.Millisecond) {
		t.Fatal("Drain reported done while the feed still held both payloads")
	}
	close(release)
	if !bus.Drain("", 5*time.Second) {
		t.Fatal("Drain did not end once the feed had read both payloads")
	}
	if len(done) != 2 {
		t.Fatalf("Drain returned with %d of 2 payloads read", len(done))
	}
}

// Drain names one session or all ("" ); a feed that stops leaves nothing
// to wait for, and other sessions' queues do not hold one session's drain.
func TestHookBusDrainPerSessionAndStoppedFeeds(t *testing.T) {
	env, _ := testEnv(t)
	bus := NewBus()
	ctx, cancel := context.WithCancel(context.Background())
	block := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = NewHook("hook", bus).Watch(ctx, sspec("hooks", map[string]any{}), env, func(harness.Record) { <-block })
	}()
	for !bus.subscribed(env.SessionID) {
		time.Sleep(time.Millisecond)
	}
	bus.Deliver(env.SessionID, Payload{Kind: BusHook, Event: "Stop", Data: json.RawMessage(`{}`)})
	bus.Deliver(env.SessionID, Payload{Kind: BusHook, Event: "Stop", Data: json.RawMessage(`{}`)})
	if !bus.Drain("s_other", 50*time.Millisecond) {
		t.Fatal("a session with nothing queued waited for another session's feed")
	}
	if bus.Drain(env.SessionID, 20*time.Millisecond) {
		t.Fatal("Drain ended with payloads still queued")
	}
	cancel()
	close(block)
	<-stopped
	if !bus.Drain("", time.Second) {
		t.Fatal("a stopped feed's queue still held Drain")
	}
}
