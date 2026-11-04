package watch

import (
	"testing"
	"time"
)

func TestSilentNeedsPriorOutput(t *testing.T) {
	var s sessionState
	now := time.Unix(100, 0)
	if s.silent(now.Add(time.Hour), 6*time.Second) {
		t.Fatal("silent without output")
	}
	s.outputSeen(now)
	if s.silent(now.Add(5*time.Second), 6*time.Second) {
		t.Fatal("silent before the window")
	}
	if !s.silent(now.Add(6*time.Second), 6*time.Second) {
		t.Fatal("not silent at the window")
	}
}

func TestOutputSeenReportsTransitionOnce(t *testing.T) {
	var s sessionState
	now := time.Unix(100, 0)
	if !s.outputSeen(now) || s.outputSeen(now) {
		t.Fatal("working transition must be reported once")
	}
}

func TestIdleFromEnv(t *testing.T) {
	t.Setenv("TOGO_IDLE_SECONDS", "0.5")
	if got := idleFromEnv(); got != 500*time.Millisecond {
		t.Fatalf("idle = %v", got)
	}
	t.Setenv("TOGO_IDLE_SECONDS", "junk")
	if got := idleFromEnv(); got != defaultIdle {
		t.Fatalf("idle = %v", got)
	}
}
