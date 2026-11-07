package local

import "github.com/gettogo/togo/helper/internal/wire"

// closeAgentPrompts closes the session's pending prompt approvals as
// answered at the desk: the agent moved on, so the user answered there.
// Asks are left alone; they belong to scripts, not the agent.
func (h *Handlers) closeAgentPrompts(id string) {
	if h.Approvals == nil {
		return
	}
	for _, a := range h.Approvals.Pending(id) {
		if a.Source != wire.SourceAsk {
			h.Approvals.CloseDesk(a.RequestID)
		}
	}
}

// setTurn records whether id has a turn under way and returns whether it
// had one.
func (h *Handlers) setTurn(id string, open bool) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.turns == nil {
		h.turns = map[string]bool{}
	}
	was := h.turns[id]
	if open {
		h.turns[id] = true
	} else {
		delete(h.turns, id)
	}
	return was
}
