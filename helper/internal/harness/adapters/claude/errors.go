package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// errorReason maps the error Claude Code tags a failed request with (the
// "error" field of an isApiErrorMessage transcript entry and of the
// StopFailure hook; one enum in 2.1.x) to an error notice reason. text is
// Claude's own message, used when the tag says nothing more specific.
func errorReason(tag, text string) string {
	switch tag {
	case "authentication_failed", "oauth_org_not_allowed", "account_on_hold", "verification_required",
		"cloud_credential_error", "billing_error":
		return harness.ErrAuth
	case "rate_limit":
		return harness.ErrRateLimit
	case "overloaded", "server_error":
		return harness.ErrProviderUnreachable
	case "model_not_found":
		return harness.ErrModelNotFound
	case "prompt_too_long":
		return harness.ErrContextOverflow
	}
	return harness.ClassifyError(text)
}

// stopFailureText is what a StopFailure hook says when the payload carries
// no message of Claude's own.
func stopFailureText(tag string) string {
	switch tag {
	case "authentication_failed":
		return "Claude Code isn't signed in or its login expired. Run /login."
	case "oauth_org_not_allowed":
		return "Your organization doesn't allow subscription access for Claude Code."
	case "account_on_hold", "verification_required":
		return "Your Anthropic account needs attention before Claude Code can continue."
	case "cloud_credential_error":
		return "Claude Code's cloud provider credentials were rejected."
	case "billing_error":
		return "The request failed on a billing problem with the account."
	case "rate_limit":
		return "Usage limit reached."
	case "overloaded":
		return "The API is overloaded. Try again shortly."
	case "server_error":
		return "Claude Code lost the connection to the API."
	case "model_not_found":
		return "The selected model doesn't exist or this account can't use it."
	case "invalid_request":
		return "The API rejected the request."
	case "max_output_tokens":
		return "The reply hit the model's output limit."
	case "prompt_too_long":
		return "The conversation is too long for the model's context window."
	}
	return "The turn stopped on an error."
}

// errorTag reads the string error tag of an entry; the system api_error
// entry carries an object there instead.
func errorTag(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// retryText describes a system api_error entry: Claude Code failed a
// request and will retry it (retryAttempt of maxRetries). The error object
// is the API error as Claude Code serialised it; its status and message
// pick the reason.
func retryText(e entry) (reason, text string) {
	var obj struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
			Error   struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"error"`
	}
	_ = json.Unmarshal(e.Error, &obj)
	words := strings.Join([]string{obj.Message, obj.Error.Message, obj.Error.Error.Type, obj.Error.Error.Message}, " ")
	if obj.Status != 0 {
		words = fmt.Sprintf("%d %s", obj.Status, words)
	}
	if words = strings.TrimSpace(words); words == "" {
		words = string(e.Error)
	}
	reason = harness.ClassifyError(words)
	what := "The API returned an error"
	switch reason {
	case harness.ErrRateLimit:
		what = "The API is rate limiting or overloaded"
	case harness.ErrProviderUnreachable:
		what = "Can't reach the API"
	case harness.ErrAuth:
		what = "The API refused the credentials"
	}
	if e.RetryAttempt > 0 && e.MaxRetries > 0 {
		return reason, fmt.Sprintf("%s; retrying (attempt %d of %d)", what, e.RetryAttempt, e.MaxRetries)
	}
	return reason, what + "; retrying"
}
