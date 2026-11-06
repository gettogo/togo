package api

import (
	"context"
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Restorable serves session.restorable.
func Restorable(s *session.Service) Method {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct{}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		list, err := s.Restorable(ctx)
		if err != nil {
			return nil, err
		}
		return wire.RestorableResult{Sessions: list}, nil
	}
}

// Restore serves session.restore. restored, when set, receives the started
// session and returns it as the app should see it (the hub publishes and
// decorates it).
func Restore(s *session.Service, restored func(context.Context, session.Session) session.Session) Method {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p wire.SessionRestoreParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		sess, err := s.Restore(ctx, p.ID, p.Resume)
		if err != nil {
			return nil, err
		}
		if restored != nil {
			sess = restored(ctx, sess)
		}
		return sess, nil
	}
}

// Dismiss serves session.dismiss.
func Dismiss(s *session.Service) Method {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p wire.SessionRefParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := s.Dismiss(ctx, p.ID); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	}
}
