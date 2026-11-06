package notify

import (
	"context"
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
)

// Protocol names.
const (
	Capability = "notify.v1"
	MethodGet  = "notify.get"
	MethodSet  = "notify.set"
)

// Methods returns notify.get and notify.set for api.API.Ext.
func (s *Store) Methods() map[string]api.Method {
	return map[string]api.Method{
		MethodGet: func(_ context.Context, raw json.RawMessage) (any, error) {
			var p struct{}
			if err := api.Decode(raw, &p); err != nil {
				return nil, err
			}
			return s.Get(), nil
		},
		MethodSet: func(_ context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				IdleSeconds   *int  `json:"idleSeconds"`
				Muted         *bool `json:"muted"`
				RemindMinutes *int  `json:"remindMinutes"`
			}
			if err := api.Decode(raw, &p); err != nil {
				return nil, err
			}
			if p.IdleSeconds == nil && p.Muted == nil && p.RemindMinutes == nil {
				return nil, &session.InvalidParamsError{Msg: "idleSeconds, muted or remindMinutes is required"}
			}
			return s.Set(Patch{IdleSeconds: p.IdleSeconds, Muted: p.Muted, RemindMinutes: p.RemindMinutes})
		},
	}
}
