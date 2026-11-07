package push

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestLiveWorkingClockPausesWhileWaiting(t *testing.T) {
	var lt LiveTracker
	t0 := time.Unix(1_000_000, 0)
	bib := "01"
	lt.Session("s1", wire.LiveWorking, &bib, "claude", "", t0)
	lt.Open("r1", t0.Add(10*time.Second), &bib, "claude", nil, t0.Add(10*time.Second))
	lt.Close("r1", t0.Add(40*time.Second))
	lt.mu.Lock()
	st, ok := lt.state()
	lt.mu.Unlock()
	if !ok {
		t.Fatal("no live state")
	}
	if want := t0.Add(30 * time.Second).Unix(); st.Since != want {
		t.Fatalf("since = %d, want %d (working clock should skip the 30s wait)", st.Since, want)
	}
}
