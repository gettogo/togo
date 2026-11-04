package hub

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
)

// ForegroundLease is how long a connection counts as foreground after the
// last request it sent. A phone that vanishes without closing its SSH
// session would otherwise keep pushes suppressed until sshd notices,
// which can take hours.
const ForegroundLease = 45 * time.Second

// Renewing returns next with every request renewing its connection's
// foreground lease.
func (h *Hub) Renewing(next rpc.Handler) rpc.Handler {
	return renewing{h: h, next: next}
}

type renewing struct {
	h    *Hub
	next rpc.Handler
}

func (r renewing) Handle(ctx context.Context, method string, params json.RawMessage) (any, *rpc.Error) {
	r.h.renew(rpc.ConnFrom(ctx))
	return r.next.Handle(ctx, method, params)
}

func (h *Hub) renew(conn *rpc.Conn) {
	if conn == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if s := h.subs[conn]; s != nil {
		s.lastRequest = h.clock()
	}
}
