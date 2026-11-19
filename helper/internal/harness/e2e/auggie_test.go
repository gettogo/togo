package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Auggie is terminal-only: the engine has no chat to follow, and the rules
// that remain are the lifecycle ones. The chat stays empty, nothing works
// or waits, and none of these events breaks the session.

func auggieRig(t *testing.T) (*enginetest.Rig, env) {
	t.Helper()
	t.Parallel()
	e := newEnv(t, time.Hour)
	if _, err := sessiongen.Write(e.home, sessiongen.Auggie{}, sessiongen.Script{Seed: 1, Start: e.start, Cwd: e.cwd}); err != nil {
		t.Fatal(err)
	}
	return e.rig(t, "auggie"), e
}

func quietState(t *testing.T, r *enginetest.Rig, where string) enginetest.State {
	t.Helper()
	st := r.Settle(fileLimit)
	if len(st.Items) != 0 || st.Mission.Summary.Working || st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("%s: %d items, summary %+v; want an empty chat and nothing working", where, len(st.Items), st.Mission.Summary)
	}
	return st
}

func TestAuggieHelperRestart(t *testing.T) {
	coverage.Covers(t, "generated", "auggie/lifecycle/helper_restart")
	r, _ := auggieRig(t)
	before := quietState(t, r, "start")
	for i := 0; i < 3; i++ {
		r.Restart()
		after := quietState(t, r, "after restart")
		fail(t, "restart", enginetest.Restarted(before, after), &sessiongen.Layout{})
	}
}

func TestAuggieExitAndRelaunch(t *testing.T) {
	coverage.Covers(t, "generated", "auggie/lifecycle/agent_exits", "auggie/lifecycle/relaunch")
	r, _ := auggieRig(t)
	r.SetStatus("working")
	r.Settle(fileLimit)
	r.SetEnded(true)
	gone := r.Settle(fileLimit)
	if gone.Mission.Summary.Working {
		t.Errorf("summary %+v after the agent exited", gone.Mission.Summary)
	}
	r.SetEnded(false)
	r.SetStatus("")
	back := r.Settle(fileLimit)
	if back.Mission.Summary.Working || back.Mission.Summary.NeedsYou != 0 {
		t.Errorf("summary %+v after the relaunch", back.Mission.Summary)
	}
	// A second exit and relaunch behave the same.
	r.SetEnded(true)
	r.Settle(fileLimit)
	r.SetEnded(false)
	r.SetStatus("")
	quietState(t, r, "after the second relaunch")
}

func TestAuggieConnectionDrop(t *testing.T) {
	coverage.Covers(t, "generated", "auggie/lifecycle/connection_drop")
	r, _ := auggieRig(t)
	ctx := context.Background()
	sub := &quietSub{done: make(chan struct{})}
	first, err := r.Engine.Open(ctx, sub, enginetest.SessionID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Engine.Close(ctx, sub, enginetest.SessionID)
	close(sub.done)
	r.SetEnded(true)
	r.SetEnded(false)
	sub2 := &quietSub{done: make(chan struct{})}
	defer close(sub2.done)
	back, err := r.Engine.Open(ctx, sub2, enginetest.SessionID, first.Cursor, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if back.Epoch != first.Epoch {
		t.Errorf("the epoch changed from %q to %q while the client was away", first.Epoch, back.Epoch)
	}
	seen := map[string]bool{}
	for _, it := range back.Items {
		if seen[it.ID] {
			t.Errorf("%s comes twice after the reconnect", it.ID)
		}
		seen[it.ID] = true
	}
}

func TestAuggieSessionRemoved(t *testing.T) {
	coverage.Covers(t, "generated", "auggie/lifecycle/session_removed")
	r, _ := auggieRig(t)
	r.SetStatus("working")
	r.Settle(fileLimit)
	r.Engine.Forget(enginetest.SessionID)
	st := r.Read()
	if len(st.Items) != 0 || st.Mission.Summary.Working {
		t.Errorf("after the removal: %d items, summary %+v", len(st.Items), st.Mission.Summary)
	}
	for _, sec := range st.Mission.Sections {
		if len(sec.Rows) != 0 {
			t.Errorf("section %s still has %d rows", sec.Kind, len(sec.Rows))
		}
	}
	// The same id served again starts clean.
	r.SetStatus("")
	r.Start()
	quietState(t, r, "served again")
}
