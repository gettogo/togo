package approval

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Methods returns approval.list and approval.answer for api.API.Ext.
// Answers arriving over the protocol connection come from the phone.
func (s *Store) Methods() map[string]api.Method {
	return map[string]api.Method{
		wire.MethodApprovalList:   s.list,
		wire.MethodApprovalAnswer: s.answer,
	}
}

func (s *Store) list(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		SessionID *string `json:"sessionId"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	var id string
	if p.SessionID != nil {
		if _, err := session.ValidateID(*p.SessionID); err != nil {
			return nil, err
		}
		id = *p.SessionID
	}
	return map[string]any{"approvals": s.Pending(id)}, nil
}

func (s *Store) answer(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		RequestID *string `json:"requestId"`
		Decision  *string `json:"decision"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	if p.RequestID == nil || p.Decision == nil {
		return nil, invalid("requestId and decision are required")
	}
	err := s.Answer(ctx, *p.RequestID, *p.Decision, wire.ByPhone)
	if errors.Is(err, ErrUnknown) {
		return nil, &api.CodeError{Code: rpc.CodeNotFound}
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"state": "answered"}, nil
}
