package surface

import (
	"context"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/rpc"
)

type openingRuntime struct {
	fakeRuntime
	entered chan struct{}
}

func (b *openingRuntime) resolve(ctx context.Context, _ string) (runtimeInfo, error) {
	close(b.entered)
	<-ctx.Done()
	return runtimeInfo{}, ctx.Err()
}

func TestTypingGuardCoversOpeningAndCanceledLease(t *testing.T) {
	b := &openingRuntime{entered: make(chan struct{})}
	s := newService(b, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := s.Open(ctx, &rpc.Conn{}, "s_abcdefghij", testScene().Options); finished <- err }()
	<-b.entered
	if !s.Held("s_abcdefghij") || s.Held("s_klmnopqrst") {
		t.Fatal("typing guard did not match the opening session")
	}
	cancel()
	if err := <-finished; err == nil {
		t.Fatal("canceled open succeeded")
	}
	if s.Held("s_abcdefghij") {
		t.Fatal("failed open left typing blocked")
	}
}

func TestTypingGuardRetiresOnlyAfterCloseJoins(t *testing.T) {
	s, _, owner, lease := nativeFixture(t)
	if !s.Held(lease.ID) {
		t.Fatal("active renderer did not block typing")
	}
	if err := s.Close(context.Background(), owner, LeaseRef{ID: lease.ID, Lease: lease.Token}); err != nil {
		t.Fatal(err)
	}
	if s.Held(lease.ID) {
		t.Fatal("closed renderer left typing blocked")
	}
}

func TestNativeAdmissionWaitsForTypingThenReleasesGateBeforeIO(t *testing.T) {
	b := &openingRuntime{entered: make(chan struct{})}
	s := newService(b, nil)
	var typing sync.Mutex
	typing.Lock()
	waiting := make(chan struct{})
	s.BeforeOpen = func(string) func() { close(waiting); typing.Lock(); return typing.Unlock }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := s.Open(ctx, &rpc.Conn{}, "s_abcdefghij", testScene().Options); finished <- err }()
	<-waiting
	if s.Held("s_abcdefghij") {
		t.Fatal("native reserved while prior typing still owned the gate")
	}
	typing.Unlock()
	<-b.entered
	if !s.Held("s_abcdefghij") {
		t.Fatal("new typing would miss native admission")
	}
	if !typing.TryLock() {
		t.Fatal("native resolution held the typing gate during IO")
	}
	typing.Unlock()
	cancel()
	if err := <-finished; err == nil {
		t.Fatal("canceled open succeeded")
	}
}
