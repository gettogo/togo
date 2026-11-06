package awake

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
)

// Protocol names.
const (
	Capability   = "awake.v1"
	MethodStatus = "awake.status"
	MethodSet    = "awake.set"
)

// Methods returns awake.status and awake.set for api.API.Ext.
func (m *Manager) Methods() map[string]api.Method {
	return map[string]api.Method{
		MethodStatus: m.statusMethod,
		MethodSet:    m.setMethod,
	}
}

func (m *Manager) statusMethod(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	return m.Status(ctx), nil
}

func (m *Manager) setMethod(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Mode         *string `json:"mode"`
		BatteryFloor *int    `json:"batteryFloor"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Mode == nil && p.BatteryFloor == nil {
		return nil, &session.InvalidParamsError{Msg: "mode or batteryFloor is required"}
	}
	var c Change
	if p.Mode != nil {
		mode, ok := ParseMode(*p.Mode)
		if !ok {
			return nil, &session.InvalidParamsError{Msg: "mode must be off, plugged or battery"}
		}
		c.Mode = &mode
	}
	if p.BatteryFloor != nil {
		if err := ValidateFloor(*p.BatteryFloor); err != nil {
			return nil, &session.InvalidParamsError{Msg: err.Error()}
		}
		c.BatteryFloor = p.BatteryFloor
	}
	st, err := m.Set(ctx, c)
	if errors.Is(err, ErrNeedsAdminSetup) {
		return nil, &api.CodeError{Code: rpc.CodeRefused, Data: map[string]string{"reason": "needs_admin_setup"}}
	}
	if err != nil {
		return nil, err
	}
	return st, nil
}
