package hub

import (
	"context"
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Methods returns events.subscribe, events.keepalive, session.setMute, and
// a session.list that reports the hub's view of each session, for
// api.API.Ext.
func (h *Hub) Methods() map[string]api.Method {
	return map[string]api.Method{
		wire.MethodEventsSubscribe: h.eventsSubscribe,
		wire.MethodEventsKeepalive: eventsKeepalive,
		wire.MethodSessionSetMute:  h.sessionSetMute,
		"session.list":             h.sessionList,
	}
}

func (h *Hub) eventsSubscribe(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Foreground *bool `json:"foreground"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	foreground := p.Foreground == nil || *p.Foreground
	return h.Subscribe(ctx, rpc.ConnFrom(ctx), foreground)
}

// eventsKeepalive does nothing itself: like every request, it renews the
// connection's foreground lease in Renewing.
func eventsKeepalive(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct{}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func (h *Hub) sessionSetMute(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID    string `json:"id"`
		Muted *bool  `json:"muted"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Muted == nil {
		return nil, &session.InvalidParamsError{Msg: "muted is required"}
	}
	return h.setMuted(ctx, p.ID, *p.Muted)
}

func (h *Hub) sessionList(ctx context.Context, _ json.RawMessage) (any, error) {
	sessions, err := h.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"sessions": h.Decorate(sessions)}, nil
}
