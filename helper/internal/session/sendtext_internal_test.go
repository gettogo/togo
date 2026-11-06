package session

import (
	"context"
	"slices"
	"testing"
	"time"
)

// fakePane records calls in order and serves screens from a script, one
// per capture; past the end it repeats the last.
type fakePane struct {
	screens []string
	calls   []string
	now     *time.Duration
}

func (f *fakePane) PasteText(_ context.Context, _, _ string) error {
	f.calls = append(f.calls, "paste")
	return nil
}

func (f *fakePane) SendKeys(_ context.Context, _ string, _ bool, keys ...string) error {
	f.calls = append(f.calls, "keys "+keys[0]+" at "+f.now.String())
	return nil
}

func (f *fakePane) CaptureHistory(context.Context, string, int, bool) (string, error) {
	f.calls = append(f.calls, "capture")
	i := min(len(f.screens)-1, countOf(f.calls, "capture")-1)
	return f.screens[i], nil
}

func countOf(calls []string, s string) int {
	n := 0
	for _, c := range calls {
		if c == s {
			n++
		}
	}
	return n
}

func fakeSleep(now *time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error { *now += d; return nil }
}

func lastCall(f *fakePane) string { return f.calls[len(f.calls)-1] }

func TestSettleFast(t *testing.T) {
	var now time.Duration
	f := &fakePane{screens: []string{"> ", "> hi", "> hi"}, now: &now}
	if err := typeReply(context.Background(), f, "s_abcdefghij", "hi", true, fakeSleep(&now)); err != nil {
		t.Fatal(err)
	}
	// The screen is calm from the first interval, so Enter waits only for
	// the minimum, rounded up to whole intervals.
	if got, want := lastCall(f), "keys Enter at 150ms"; got != want {
		t.Fatalf("last call %q, want %q", got, want)
	}
}

func TestSettleNeverSettles(t *testing.T) {
	var now time.Duration
	screens := make([]string, 30)
	for i := range screens {
		screens[i] = "spinner " + string(rune('a'+i))
	}
	f := &fakePane{screens: screens, now: &now}
	if err := typeReply(context.Background(), f, "s_abcdefghij", "hi", true, fakeSleep(&now)); err != nil {
		t.Fatal(err)
	}
	if got, want := lastCall(f), "keys Enter at 500ms"; got != want {
		t.Fatalf("last call %q, want %q", got, want)
	}
}

func TestSettleOnLastLineVisible(t *testing.T) {
	var now time.Duration
	f := &fakePane{screens: []string{"a", "b", "c", "d last line here", "e last line here"}, now: &now}
	if err := typeReply(context.Background(), f, "s_abcdefghij", "first\nlast line here", true, fakeSleep(&now)); err != nil {
		t.Fatal(err)
	}
	if got, want := lastCall(f), "keys Enter at 150ms"; got != want {
		t.Fatalf("last call %q, want %q", got, want)
	}
}

func TestEnterOnlyAfterPaste(t *testing.T) {
	var now time.Duration
	f := &fakePane{screens: []string{"x"}, now: &now}
	if err := typeReply(context.Background(), f, "s_abcdefghij", "hi", true, fakeSleep(&now)); err != nil {
		t.Fatal(err)
	}
	if f.calls[0] != "paste" || !slices.Contains(f.calls[1:], "capture") || lastCall(f)[:4] != "keys" {
		t.Fatalf("calls %q, want paste, captures, then Enter", f.calls)
	}
	f = &fakePane{screens: []string{"x"}, now: &now}
	if err := typeReply(context.Background(), f, "s_abcdefghij", "hi", false, fakeSleep(&now)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.calls, []string{"paste"}) {
		t.Fatalf("without submit calls %q, want only the paste", f.calls)
	}
}

func TestIsShell(t *testing.T) {
	for _, tc := range []struct {
		cmd, login string
		want       bool
	}{
		{"sh", "/bin/zsh", true}, {"bash", "/bin/sh", true}, {"-zsh", "/bin/zsh", true},
		{"zsh", "/bin/zsh", true}, {"fish", "/bin/zsh", true}, {"-fish", "/bin/zsh", true},
		{"dash", "/bin/zsh", true}, {"ksh", "/bin/zsh", true}, {"tcsh", "/bin/zsh", true},
		{"csh", "/bin/zsh", true}, {"nu", "/bin/zsh", true}, {"pwsh", "/bin/zsh", true},
		{"-bash", "/bin/zsh", true}, {"xonsh", "/usr/bin/xonsh", true},
		{"claude", "/bin/zsh", false}, {"node", "/bin/zsh", false}, {"cat", "/bin/sh", false},
		{"bashful", "/bin/zsh", false}, {"", "/bin/zsh", false},
	} {
		if got := isShell(tc.cmd, tc.login); got != tc.want {
			t.Errorf("isShell(%q, %q) = %v, want %v", tc.cmd, tc.login, got, tc.want)
		}
	}
}
