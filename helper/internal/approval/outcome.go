package approval

import (
	"errors"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Outcome names what became of an answer from the error Answer returned,
// in the words an answer push carries.
func Outcome(err error) string {
	if err == nil {
		return wire.AnswerApplied
	}
	var ce *api.CodeError
	if !errors.As(err, &ce) {
		return wire.AnswerFailed
	}
	switch ce.Code {
	case rpc.CodeConflict:
		if c, ok := ce.Data.(Conflict); ok {
			switch c.Reason {
			case wire.ConflictAnswered, wire.ConflictPromptGone, wire.ConflictExpired:
				return c.Reason
			}
		}
	case rpc.CodeRefused:
		if d, ok := ce.Data.(map[string]string); ok && d["reason"] == wire.RefusedUnconfirmed {
			return wire.AnswerUnconfirmed
		}
	}
	return wire.AnswerFailed
}
