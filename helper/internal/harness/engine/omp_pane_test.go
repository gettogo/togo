package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// Seen on omp 19.0.1: resume A, then B, then A again, and tmux gives the
// pane A's old device back. omp leaves that device's record as it is (it
// already names A), so its time is older than the pane's process. The chat
// still moves to A: the pane's command resumes A.
func TestOmpResumeBackOntoAReusedDeviceWithAnUnchangedRecord(t *testing.T) {
	r := newOmpRig(t, "s_omppane01")
	r.record("/dev/ttys011", ompConvAFile)
	r.next = []string{"/dev/ttys012", "/dev/ttys011"}
	r.onRelaun = func(argv []string, tty string) {
		if argv[len(argv)-1] == ompConvA {
			r.record(tty, ompConvAFile)
		} else {
			r.record(tty, ompConvBFile)
		}
	}
	r.start()
	r.waitShowsOnly(ompConvAText, ompConvBText)
	if res := r.resume(ompConvB); res.Result != harness.ResultRelaunched {
		t.Fatalf("resume B = %+v", res)
	}
	r.waitShowsOnly(ompConvBText, ompConvAText)
	if res := r.resume(ompConvA); res.Result != harness.ResultRelaunched {
		t.Fatalf("resume A = %+v", res)
	}
	r.waitShowsOnly(ompConvAText, ompConvBText)
}

// The pane respawned by hand (tmux respawn-pane -k) with no exit the hub
// saw: the periodic pane check finds the new device and process.
func TestOmpPaneRespawnedByHandIsFollowed(t *testing.T) {
	r := newOmpRig(t, "s_omppane02")
	r.record("/dev/ttys011", ompConvAFile)
	r.next = []string{"/dev/ttys012"}
	r.start()
	r.waitShowsOnly(ompConvAText, ompConvBText)
	r.respawn([]string{"omp", "--resume", ompConvB})
	r.record("/dev/ttys012", ompConvBFile)
	r.waitShowsOnly(ompConvBText, ompConvAText)
}

// After a helper restart the session's pane is read from tmux and the
// process again: its device, start and command come back without anything
// kept from before.
func TestRebuiltSessionReadsThePaneAgain(t *testing.T) {
	r := newOmpRig(t, "s_omppane03")
	r.respawn([]string{"omp", "--resume", ompConvB})
	r.mu.Lock()
	started := r.started
	r.mu.Unlock()
	info, _ := r.e.cfg.Sessions.Resolve(context.Background(), r.id)
	env := r.e.envFor(info)
	if env.Vars[source.VarTTY] != "/dev/ttys011" || env.Vars[source.VarPanePID] != "1001" ||
		env.Vars[source.VarPaneStarted] != started.UTC().Format(time.RFC3339Nano) ||
		!strings.HasSuffix(env.Vars[source.VarPaneCommand], "omp --resume "+ompConvB) {
		t.Fatalf("env vars %v", env.Vars)
	}
}
