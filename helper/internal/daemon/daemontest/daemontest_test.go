package daemontest

import (
	"os"
	"testing"
	"time"
)

func TestStartFakeCleanupWaitsForRun(t *testing.T) {
	home, err := os.MkdirTemp("", "tg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	var fake *Fake
	t.Run("fixture", func(t *testing.T) {
		fake = StartFake(t, home, "test", nil)
	})
	select {
	case err := <-fake.Done:
		if err != nil {
			t.Errorf("Run = %v, want nil after cleanup", err)
		}
	default:
		t.Error("fixture cleanup returned before Run completed")
		select {
		case <-fake.Done:
		case <-time.After(30 * time.Second):
			t.Fatal("daemon did not stop after cleanup")
		}
	}
}
