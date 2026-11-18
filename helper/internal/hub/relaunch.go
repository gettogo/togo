package hub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// SessionRelaunch serves session.relaunch: it runs the session's agent
// again when its pane shows the agent gone, clears the exit and publishes
// the session. A live agent is -32006 running. Relaunches of one session
// run one at a time, and one that follows another before the new run has
// exited or sent a hook is refused as running too: while the new run's
// login shell starts, the pane would still look as if the agent were gone.
func (h *Hub) SessionRelaunch(ctx context.Context, raw json.RawMessage) (any, error) {
	var p wire.SessionRefParams
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	// Relaunch itself refuses other ids; only a managed id gets a lock.
	if managed, err := session.ValidateID(p.ID); err == nil && managed {
		mu, _ := h.relaunching.LoadOrStore(p.ID, &sync.Mutex{})
		mu.(*sync.Mutex).Lock()
		defer mu.(*sync.Mutex).Unlock()
		if h.starting(p.ID) {
			return nil, errRunning()
		}
	}
	sess, err := h.svc.Relaunch(ctx, p.ID)
	if errors.Is(err, session.ErrRunning) {
		return nil, errRunning()
	}
	if err != nil {
		return nil, err
	}
	h.markRelaunched(p.ID)
	return h.PublishSession(sess), nil
}

func errRunning() error {
	return &api.CodeError{Code: rpc.CodeConflict, Data: map[string]string{"reason": wire.ConflictRunning}}
}

// starting reports whether the session was relaunched and its new run has
// neither sent a hook nor exited since.
func (h *Hub) starting(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.sessions[id]
	return ok && st.awaitRun && st.exitStatus == ""
}

// markRelaunched clears the session's exit and derived status and waits
// for the new run's first hook before taking exit hooks again.
func (h *Hub) markRelaunched(id string) {
	h.exitMu.Lock()
	defer h.exitMu.Unlock()
	h.mu.Lock()
	st := h.stateLocked(id)
	st.exitStatus, st.exit, st.exitKnown, st.exitRestored = "", nil, true, false
	st.status, st.activity = "", nil
	st.awaitRun = true
	h.mu.Unlock()
	h.saveExit(id, nil)
}

// relaunchExitGrace is how long after a relaunch an exit report is taken
// for the killed run's: respawning the pane ends the old wrapper, which
// reports its exit at once, while the new run's own failure (a bad flag,
// "No conversation found") takes longer to start and fail.
const relaunchExitGrace = 2 * time.Second

// Relaunched records that the agent is being restarted in its pane by
// something other than session.relaunch (an adapter action with new
// flags): the old run's exit report must not mark the session ended.
// session.relaunch needs no grace: it starts an agent already gone. The
// caller runs undo when the restart did not happen.
func (h *Hub) Relaunched(id string) (undo func()) {
	h.mu.Lock()
	st := h.stateLocked(id)
	status, exit, restored := st.exitStatus, st.exit, st.exitRestored
	h.mu.Unlock()
	h.markRelaunched(id)
	h.mu.Lock()
	h.stateLocked(id).relaunchedAt = h.clock()
	h.mu.Unlock()
	return func() { h.relaunchFailed(id, status, exit, restored) }
}

// relaunchFailed puts back what Relaunched cleared. Nothing was started,
// so no hook or exit of a new run is coming: left marked, an ended session
// would read idle with no agent in it and refuse a relaunch as running,
// and a live agent's real exit would be dropped.
func (h *Hub) relaunchFailed(id, status string, exit *wire.SessionExit, restored bool) {
	h.exitMu.Lock()
	h.mu.Lock()
	st := h.stateLocked(id)
	st.awaitRun, st.relaunchedAt = false, time.Time{}
	back := status != "" && st.exitStatus == ""
	if back {
		st.exitStatus, st.exit, st.exitKnown, st.exitRestored = status, exit, true, restored
	}
	h.mu.Unlock()
	if back {
		h.saveExit(id, &session.SavedExit{Status: status, Exit: *exit})
	}
	h.exitMu.Unlock()
	if back {
		h.publishSession(id)
	}
}
