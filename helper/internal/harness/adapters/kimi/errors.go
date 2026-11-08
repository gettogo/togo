package kimi

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/internal/errtext"
)

// failTurn ends a turn Kimi recorded as failed or blocked and says why.
// The error event Kimi also emits is not written to the wire log; the
// reason is on turn.ended's error: {code, name, message, details}. Codes
// are from Kimi Code's agent core (codeForStatusError and its tables).
func (w *Wire) failTurn(r record, at time.Time) ([]harness.Item, []harness.Op) {
	tid := ""
	if w.turn != nil {
		tid = w.turn.id
	}
	items, ops := w.endTurn(harness.StateFailed, at)
	n := harness.Item{ID: "error-" + strconv.FormatInt(r.Time, 10), TurnID: tid, Kind: harness.KindNotice, At: at,
		Notice: failNotice(r)}
	return append(items, n), ops
}

func failNotice(r record) *harness.NoticeBody {
	e := r.Error
	if e == nil {
		if r.Reason == "blocked" {
			return harness.ErrorNotice(harness.ErrRefused, "Kimi blocked this prompt")
		}
		return harness.ErrorNotice(harness.ErrOther, "The turn failed")
	}
	code := strings.ToLower(e.Code)
	switch {
	case e.Name == "OAuthUnauthorizedError" || code == "auth.login_required":
		return errtext.With(harness.ErrAuth, "Kimi needs you to sign in again (run /login)", e.Message)
	case code == "provider.auth_error" || strings.HasPrefix(code, "auth."):
		return errtext.Of(harness.ErrAuth, e.Message)
	case code == "provider.rate_limit" || strings.Contains(code, "quota") || strings.Contains(e.Name, "Quota"):
		return errtext.Of(harness.ErrRateLimit, e.Message)
	case code == "provider.overloaded":
		return errtext.With(harness.ErrProviderUnreachable, "The provider is overloaded", e.Message)
	case code == "provider.connection_error":
		return errtext.Of(harness.ErrProviderUnreachable, e.Message)
	case code == "context.overflow":
		return errtext.Of(harness.ErrContextOverflow, e.Message)
	case strings.HasPrefix(code, "model."):
		return errtext.Of(harness.ErrModelNotFound, e.Message)
	case code == "loop.max_steps_exceeded":
		lead := "Stopped at the step limit"
		if e.Details.MaxSteps > 0 {
			lead = fmt.Sprintf("Stopped at the step limit (%d)", e.Details.MaxSteps)
		}
		return errtext.With(harness.ErrOther, lead, "")
	case code == "provider.filtered":
		return errtext.With(harness.ErrOther, "The provider's content filter stopped the reply", e.Message)
	}
	return errtext.Notice(e.Message, e.Details.StatusCode, "The turn failed")
}
