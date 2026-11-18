package surface

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
)

// Capability is additive to protocol 1.
const Capability = "surface.v1"

// Concurrent keeps surface disk and pane operations off the RPC read loop.
var Concurrent = []string{"surface.open", "surface.read", "surface.input", "surface.ack", "surface.close"}

// Methods exposes a renderer only to its authenticated connection.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{
		"surface.open": func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				ID      string        `json:"id"`
				Options RenderOptions `json:"options"`
			}
			if err := api.Decode(raw, &p); err != nil {
				return nil, err
			}
			result, err := s.Open(ctx, rpc.ConnFrom(ctx), p.ID, p.Options)
			return result, methodError(err)
		},
		"surface.read": func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p ReadParams
			if err := api.Decode(raw, &p); err != nil {
				return nil, err
			}
			result, err := s.Read(rpc.ConnFrom(ctx), p)
			return result, methodError(err)
		},
		"surface.input": func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p InputParams
			if err := api.Decode(raw, &p); err != nil {
				return nil, err
			}
			result, err := s.Input(ctx, rpc.ConnFrom(ctx), p)
			return result, methodError(err)
		},
		"surface.ack": func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p AckParams
			if err := api.Decode(raw, &p); err != nil {
				return nil, err
			}
			return struct{}{}, methodError(s.Ack(ctx, rpc.ConnFrom(ctx), p))
		},
		"surface.close": func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p LeaseRef
			if err := api.Decode(raw, &p); err != nil {
				return nil, err
			}
			return struct{}{}, methodError(s.Close(ctx, rpc.ConnFrom(ctx), p))
		},
	}
}

func methodError(err error) error {
	if err == nil {
		return nil
	}
	reason := "unavailable"
	switch {
	case errors.Is(err, errLease), errors.Is(err, errRuntimeChanged):
		reason = "changed"
	case errors.Is(err, errBusy):
		reason = "busy"
	case errors.Is(err, ErrCachePending):
		reason = "pending"
	case errors.Is(err, ErrMalformed), errors.Is(err, ErrLimit):
		reason = "invalid"
	}
	return &api.CodeError{Code: rpc.CodeRefused, Data: map[string]string{"reason": reason}}
}
