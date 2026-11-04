package tap

import (
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func feedAll(chunks ...string) []wire.Signal {
	var s Scanner
	var out []wire.Signal
	for _, c := range chunks {
		out = append(out, s.Feed([]byte(c))...)
	}
	return out
}

func TestBellAlone(t *testing.T) {
	got := feedAll("hello\a world")
	want := []wire.Signal{{Kind: wire.SignalBell}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestBelTerminatedOSCIsNotABell(t *testing.T) {
	if got := feedAll("\x1b]0;title\a"); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
	if got := feedAll("\x1bP1;2;3\a\x1b\\"); len(got) != 0 {
		t.Fatalf("DCS content produced %+v", got)
	}
}

func TestOSC9Notification(t *testing.T) {
	got := feedAll("\x1b]9;Build done\x1b\\")
	want := []wire.Signal{{Kind: wire.SignalOSC9, Body: "Build done"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestOSC9ProgressIgnored(t *testing.T) {
	if got := feedAll("\x1b]9;4;1;50\a"); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestOSC777Notify(t *testing.T) {
	got := feedAll("\x1b]777;notify;Title;Body; with semicolon\a")
	want := []wire.Signal{{Kind: wire.SignalOSC777, Title: "Title", Body: "Body; with semicolon"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := feedAll("\x1b]777;precmd\a"); len(got) != 0 {
		t.Fatalf("non-notify 777 gave %+v", got)
	}
}

const fixtureStream = "a\a" + "\x1b]0;t\a" + "\x1b]9;one\x1b\\" + "x\x1b[31m" +
	"\x1b]777;notify;T;B\a" + "\x1b]9;4;3\a" + "\x1bPq\a\x1b\\" + "\a" + "\x1b]9;two\a"

func TestSequenceSplitAcrossFeeds(t *testing.T) {
	want := feedAll(fixtureStream)
	if len(want) != 5 {
		t.Fatalf("fixture gave %d signals: %+v", len(want), want)
	}
	for i := 0; i <= len(fixtureStream); i++ {
		got := feedAll(fixtureStream[:i], fixtureStream[i:])
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("split at %d: got %+v", i, got)
		}
	}
	var s Scanner
	var got []wire.Signal
	for i := 0; i < len(fixtureStream); i++ {
		got = append(got, s.Feed([]byte{fixtureStream[i]})...)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("byte at a time: got %+v", got)
	}
}

func TestOversizedOSCDiscarded(t *testing.T) {
	var s Scanner
	big := make([]byte, 1<<20)
	for i := range big {
		big[i] = 'a'
	}
	got := s.Feed([]byte("\x1b]9;"))
	got = append(got, s.Feed(big)...)
	if cap(s.body) > 2*maxOSCBody {
		t.Fatalf("body buffer grew to %d", cap(s.body))
	}
	got = append(got, s.Feed([]byte("\a"))...)
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
	// The scanner recovers for the next sequence.
	if got := s.Feed([]byte("\x1b]9;ok\a")); len(got) != 1 || got[0].Body != "ok" {
		t.Fatalf("after overflow: %+v", got)
	}
}

func TestControlCharsStripped(t *testing.T) {
	got := feedAll("\x1b]9;a\x01b\x7f\a")
	if len(got) != 1 || got[0].Body != "ab" {
		t.Fatalf("got %+v", got)
	}
}

func FuzzScannerNeverPanics(f *testing.F) {
	f.Add([]byte(fixtureStream))
	f.Add([]byte("\x1b]777;notify;\xff;\xfe\a"))
	f.Fuzz(func(t *testing.T, p []byte) {
		var s Scanner
		whole := s.Feed(p)
		var s2 Scanner
		var split []wire.Signal
		mid := len(p) / 2
		split = append(split, s2.Feed(p[:mid])...)
		split = append(split, s2.Feed(p[mid:])...)
		if !reflect.DeepEqual(whole, split) {
			t.Fatalf("split changed result: %+v vs %+v", whole, split)
		}
		if cap(s.body) > 4*maxOSCBody {
			t.Fatalf("body buffer grew to %d", cap(s.body))
		}
	})
}
