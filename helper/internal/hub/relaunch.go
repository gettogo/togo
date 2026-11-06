package hub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

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
