package approval

import (
	"context"
	"errors"
	"testing"
)

// flakyTmux fails its first reads, as tmux does when it cannot be started
// for a moment.
type flakyTmux struct {
	fakeTmux
	fail  int
	reads int
}

func (f *flakyTmux) CapturePane(ctx context.Context, tgt string) (string, error) {
	f.reads++
	if f.reads <= f.fail {
		return "", errors.New("fork/exec tmux: resource temporarily unavailable")
	}
	return f.fakeTmux.CapturePane(ctx, tgt)
}

func TestFirstCaptureReadsAgainAfterAFailedRead(t *testing.T) {
	tm := &flakyTmux{fail: 2}
	tm.screen = "Do you want to proceed?"
	st := &Store{cfg: Config{Tmux: tm}}
	got, err := st.firstCapture(context.Background(), "s_aaaaaaaaaa")
	if err != nil || got != tm.screen {
		t.Fatalf("firstCapture = %q, %v; want the screen after two failed reads", got, err)
	}
}

func TestFirstCaptureGivesUp(t *testing.T) {
	tm := &flakyTmux{fail: 99}
	st := &Store{cfg: Config{Tmux: tm}}
	if _, err := st.firstCapture(context.Background(), "s_aaaaaaaaaa"); err == nil {
		t.Fatal("no error after every read failed")
	}
	if tm.reads != 1+len(captureRetries) {
		t.Fatalf("read %d times, want %d", tm.reads, 1+len(captureRetries))
	}
}
