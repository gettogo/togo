package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestShutdownWatchWaitsForOriginalConnection(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	watch := &ShutdownWatch{conn: client}
	done := make(chan error, 1)
	go func() { done <- watch.Wait(context.Background()) }()
	if _, err := server.Write([]byte("ignored notification")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("finished before shutdown: %v", err)
	default:
	}
	server.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestShutdownWatchHonoursCancellation(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	watch := &ShutdownWatch{conn: client}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := watch.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}

func TestShutdownWatchHonoursDeadline(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	watch := &ShutdownWatch{conn: client}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := watch.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want deadline", err)
	}
}

func TestShutdownWatchDoesNotMistakeReadFailureForShutdown(t *testing.T) {
	client, server := net.Pipe()
	server.Close()
	client.Close()
	watch := &ShutdownWatch{conn: client}
	if err := watch.Wait(context.Background()); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want read failure", err)
	}
}
