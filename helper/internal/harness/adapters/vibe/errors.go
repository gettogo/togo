package vibe

import (
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/internal/errtext"
)

// failNotice says why a turn failed, from latestTurn.error: {code,
// message, details: {httpStatus, reason, exceptionType}}. Codes are from
// Vibe 3.1's local harness: model_unauthorized (401/403),
// context_overflow and the compaction codes when the conversation cannot
// fit, model_stream_failed for other HTTP errors (404 for a model the
// provider lacks). Rate limits and unreachable providers are retried
// until the user interrupts, so they mostly end interrupted, not failed.
func failNotice(e *turnError) *harness.NoticeBody {
	if e == nil {
		return harness.ErrorNotice(harness.ErrOther, "The turn failed")
	}
	code, d := strings.ToLower(e.Code), e.Details
	status := d.HTTPStatus
	switch {
	case code == "model_unauthorized" || d.Reason == "http_unauthorized" || d.Reason == "http_forbidden" ||
		status == 401 || status == 403:
		return errtext.Of(harness.ErrAuth, e.Message)
	case code == "context_overflow" || strings.HasPrefix(code, "model_context") || strings.HasPrefix(code, "compaction_"):
		return errtext.Of(harness.ErrContextOverflow, e.Message)
	case status == 429 || strings.Contains(code, "rate_limit"):
		return errtext.Of(harness.ErrRateLimit, e.Message)
	case status == 404:
		return errtext.Of(harness.ErrModelNotFound, e.Message)
	case strings.Contains(d.ExceptionType, "Connect") || strings.Contains(d.ExceptionType, "Timeout") || status >= 500:
		return errtext.Of(harness.ErrProviderUnreachable, e.Message)
	case code == "model_refused":
		return errtext.With(harness.ErrOther, "The model refused", e.Message)
	}
	return errtext.Notice(e.Message, status, "The turn failed")
}
