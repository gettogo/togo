package opencode

import (
	"fmt"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Provider errors, as OpenCode and Kilo report them.
//
// OpenCode 1.x and Kilo name the error of an assistant message and of
// session.error: ProviderAuthError, APIError (with statusCode),
// ContextOverflowError, ContentFilterError, MessageOutputLengthError,
// StructuredOutputError, UnknownError and MessageAbortedError.
//
// OpenCode 2.x types the error of session.step.failed,
// session.execution.failed and session.retry.scheduled (tR in the 2.2.5
// bundle): provider.auth, provider.rate-limit, provider.quota,
// provider.free-limit, provider.chatgpt-limit, provider.content-filter,
// provider.transport, provider.timeout, provider.internal,
// provider.invalid-output, provider.invalid-request (with status),
// provider.unsupported-operation, provider.no-route, provider.unknown and
// tool.execution, for example
//
//	{"type":"provider.transport","message":"ECONNREFUSED: Unable to connect. Is the computer able to access the url?"}
//	{"type":"provider.invalid-request","message":"no router for requested model","status":404}

// errorReason picks the notice reason for an error name or type, its
// HTTP status and its message.
func errorReason(name string, status int, msg string) string {
	switch name {
	case "ProviderAuthError", "provider.auth":
		return harness.ErrAuth
	case "ContextOverflowError":
		return harness.ErrContextOverflow
	case "provider.rate-limit", "provider.quota", "provider.free-limit", "provider.chatgpt-limit":
		return harness.ErrRateLimit
	case "provider.transport", "provider.timeout", "provider.internal":
		return harness.ErrProviderUnreachable
	case "provider.no-route", "provider.unknown":
		return harness.ErrModelNotFound
	}
	switch {
	case status == 401 || status == 403:
		return harness.ErrAuth
	case status == 429:
		return harness.ErrRateLimit
	}
	r := harness.ClassifyError(msg)
	if r == harness.ErrOther && status == 404 {
		return harness.ErrModelNotFound
	}
	if r == harness.ErrOther && status >= 500 {
		return harness.ErrProviderUnreachable
	}
	return r
}

// errorBody is the notice for a provider error: a short sentence for its
// reason, then the provider's own words.
func errorBody(name string, status int, msg string) *harness.NoticeBody {
	msg = strings.TrimSpace(msg)
	reason := errorReason(name, status, msg)
	lead := map[string]string{
		harness.ErrAuth:                "The provider refused the API key",
		harness.ErrProviderUnreachable: "Can't reach the provider",
		harness.ErrRateLimit:           "The provider's rate limit or quota was hit",
		harness.ErrModelNotFound:       "The provider doesn't have this model",
		harness.ErrContextOverflow:     "The conversation is too long for the model",
	}[reason]
	if lead == "" {
		switch name {
		case "ContentFilterError", "provider.content-filter":
			lead = "The provider's content filter blocked the reply"
		case "MessageOutputLengthError":
			lead = "The reply hit the model's output limit"
		case "tool.execution":
			lead = "A tool failed"
		}
	}
	switch {
	case lead == "" && msg == "":
		return harness.ErrorNotice(reason, "The agent stopped on an error ("+name+").")
	case lead == "":
		return harness.ErrorNotice(reason, msg)
	case msg == "":
		return harness.ErrorNotice(reason, lead+".")
	}
	return harness.ErrorNotice(reason, lead+": "+msg)
}

// retryBody is the notice for a provider call the agent will try again.
func retryBody(name string, status int, msg string, attempt int) *harness.NoticeBody {
	b := errorBody(name, status, msg)
	if attempt > 1 {
		b.Text = truncate(fmt.Sprintf("%s (retrying, attempt %d)", strings.TrimSuffix(b.Text, "."), attempt), harness.MaxNoticeText)
	} else {
		b.Text = truncate(strings.TrimSuffix(b.Text, ".")+" (retrying)", harness.MaxNoticeText)
	}
	return b
}
