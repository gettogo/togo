package activity

import (
	"testing"
	"time"
)

func TestUnknownUntilOutput(t *testing.T) {
	tr := New()
	if got := tr.History("s", time.Unix(1000, 0)); got != nil {
		t.Fatalf("History = %v, want nil", got)
	}
}

func TestBucketsAreNormalisedSecondsWithOutput(t *testing.T) {
	tr := New()
	base := time.Unix(30*1000, 0) // bucket aligned
	for i := 0; i < 15; i++ {
		tr.Record("s", base.Add(time.Duration(i)*time.Second))
	}
	got := tr.History("s", base.Add(20*time.Second))
	if len(got) != Buckets {
		t.Fatalf("len = %d, want %d", len(got), Buckets)
	}
	if got[Buckets-1] != 0.5 {
		t.Fatalf("newest = %v, want 0.5", got[Buckets-1])
	}
	for _, v := range got[:Buckets-1] {
		if v != 0 {
			t.Fatalf("older bucket = %v, want 0", v)
		}
	}
}

func TestSameSecondCountsOnce(t *testing.T) {
	tr := New()
	at := time.Unix(30*1000, 0)
	tr.Record("s", at)
	tr.Record("s", at.Add(100*time.Millisecond))
	got := tr.History("s", at)
	if want := 1.0 / 30; got[Buckets-1] != want {
		t.Fatalf("newest = %v, want %v", got[Buckets-1], want)
	}
}

func TestOldestFirstAndShift(t *testing.T) {
	tr := New()
	base := time.Unix(30*1000, 0)
	for i := 0; i < 30; i++ {
		tr.Record("s", base.Add(time.Duration(i)*time.Second))
	}
	got := tr.History("s", base.Add(30*time.Second+time.Second))
	if got[Buckets-2] != 1 || got[Buckets-1] != 0 {
		t.Fatalf("tail = %v %v, want 1 0", got[Buckets-2], got[Buckets-1])
	}
}

func TestOlderThanWindowFallsOff(t *testing.T) {
	tr := New()
	base := time.Unix(30*1000, 0)
	tr.Record("s", base)
	got := tr.History("s", base.Add(time.Duration(Buckets)*BucketSpan))
	for _, v := range got {
		if v != 0 {
			t.Fatalf("history = %v, want all zero", got)
		}
	}
}

func TestForgetDropsSession(t *testing.T) {
	tr := New()
	tr.Record("s", time.Unix(1000, 0))
	tr.Forget("s")
	if got := tr.History("s", time.Unix(1000, 0)); got != nil {
		t.Fatalf("History = %v, want nil", got)
	}
}

func TestRollingSendsLiveSessionsAndClearsOnce(t *testing.T) {
	tr := New()
	base := time.Unix(30*1000, 0)
	tr.Record("s", base)
	if got := tr.Rolling(base.Add(BucketSpan)); len(got) != 1 || got[0] != "s" {
		t.Fatalf("Rolling while live = %v, want [s]", got)
	}
	gone := base.Add(BucketSpan * Buckets)
	if got := tr.Rolling(gone); len(got) != 1 {
		t.Fatalf("Rolling as output leaves the window = %v, want [s] once", got)
	}
	if got := tr.Rolling(gone.Add(BucketSpan)); len(got) != 0 {
		t.Fatalf("Rolling after clearing = %v, want none", got)
	}
}

func TestTrackerIsBounded(t *testing.T) {
	tr := New()
	base := time.Unix(30*1000, 0)
	for i := 0; i <= MaxSessions; i++ {
		tr.Record(string(rune('a'+i%26))+time.Duration(i).String(), base.Add(time.Duration(i)*time.Second))
	}
	if n := len(tr.by); n != MaxSessions {
		t.Fatalf("tracked = %d, want %d", n, MaxSessions)
	}
}
