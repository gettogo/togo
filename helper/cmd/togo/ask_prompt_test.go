package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"
)

// /dev/null is a character device but not a terminal: with no daemon, ask
// must report the daemon unreachable, not a timeout.
func TestAskDevNullStdinIsUnreachable(t *testing.T) {
	home, _ := tapHome(t)
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip(err)
	}
	defer null.Close()
	var out, errb bytes.Buffer
	code := runContext(context.Background(), []string{"ask", "Deploy?"}, map[string]string{"TOGO_HOME": home}, null, &out, &errb)
	if code != exitUnreachable {
		t.Fatalf("code %d, want %d; stderr %q", code, exitUnreachable, errb.String())
	}
}

// Ctrl-C at the terminal prompt ends the command at once with status 130.
func TestAskPromptStopsOnInterrupt(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var out, errb bytes.Buffer
	go func() { done <- askHere(ctx, pr, &out, &errb, "Deploy?", []string{"Yes", "No"}, "No") }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code != exitInterrupted {
			t.Fatalf("code %d, want %d", code, exitInterrupted)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("prompt kept waiting after the interrupt")
	}
}
