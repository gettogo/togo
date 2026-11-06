package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// SendText serves session.sendText. decorate fills a session's status and
// exit as the app sees them (the hub's Decorate); nil leaves what tmux
// reports. A refusal is -32006 with its reason.
func SendText(s *session.Service, decorate func([]session.Session) []session.Session) Method {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p wire.SessionSendTextParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if managed, err := session.ValidateID(p.ID); err != nil {
			return nil, err
		} else if !managed {
			return nil, session.ErrNotManaged
		}
		if err := session.ValidateSendText(p.Text); err != nil {
			return nil, err
		}
		sess, err := s.Resolve(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if decorate != nil {
			sess = decorate([]session.Session{sess})[0]
		}
		err = s.SendText(ctx, p.ID, p.Text, p.Submit, sess.Status, sess.Exit)
		if refused, ok := errors.AsType[*session.ReplyRefusedError](err); ok {
			return nil, &CodeError{Code: rpc.CodeConflict, Data: map[string]string{"reason": refused.Reason}}
		}
		if err != nil {
			return nil, err
		}
		return struct{}{}, nil
	}
}
