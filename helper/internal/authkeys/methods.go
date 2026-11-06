package authkeys

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Methods returns authkeys.remove for api.API.Ext. path names the
// authorized_keys file, normally DefaultPath; the request never supplies a
// path.
func Methods(path func() (string, error)) map[string]api.Method {
	return map[string]api.Method{
		wire.MethodAuthkeysRemove: func(_ context.Context, raw json.RawMessage) (any, error) {
			var p wire.AuthkeysRemoveParams
			if err := api.Decode(raw, &p); err != nil {
				return nil, err
			}
			if _, _, err := ParseKey(p.PublicKey); err != nil {
				return nil, &session.InvalidParamsError{Msg: "key"}
			}
			file, err := path()
			if err != nil {
				return nil, err
			}
			n, err := Remove(file, p.PublicKey)
			var c *Conflict
			if errors.As(err, &c) {
				return nil, &api.CodeError{Code: rpc.CodeConflict, Data: map[string]string{"reason": c.Reason}}
			}
			if err != nil {
				return nil, err
			}
			return wire.AuthkeysRemoveResult{Removed: n}, nil
		},
	}
}
