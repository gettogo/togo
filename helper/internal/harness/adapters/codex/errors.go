package codex

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// turnError is a TurnError of the app-server protocol (v2 schema, Codex
// 0.16x): the message Codex shows and codexErrorInfo, either a bare code
// ("unauthorized", "usageLimitExceeded", ...) or an object for HTTP
// failures ({"httpConnectionFailed": {"httpStatusCode": 502}}).
type turnError struct {
	Message           string          `json:"message"`
	CodexErrorInfo    json.RawMessage `json:"codexErrorInfo"`
	AdditionalDetails *string         `json:"additionalDetails"`
	// Misalignment explains a policy block (Codex 0.150 and later).
	Misalignment *struct {
		DetailedExplanation *string `json:"detailedExplanation"`
	} `json:"misalignment"`
}

// errorReason maps codexErrorInfo to an error notice reason; codes that
// name no cause fall back to the message's words.
func errorReason(e turnError) string {
	var code string
	status := 0
	if json.Unmarshal(e.CodexErrorInfo, &code) != nil {
		var obj map[string]struct {
			Status *int `json:"httpStatusCode"`
		}
		if json.Unmarshal(e.CodexErrorInfo, &obj) == nil {
			for k, v := range obj {
				code = k
				if v.Status != nil {
					status = *v.Status
				}
			}
		}
	}
	switch status {
	case 401, 403:
		return harness.ErrAuth
	case 429:
		return harness.ErrRateLimit
	}
	switch code {
	case "unauthorized":
		return harness.ErrAuth
	case "usageLimitExceeded", "rateLimitExceeded", "sessionBudgetExceeded", "policyCapacityUnavailable", "flexUnavailable":
		return harness.ErrRateLimit
	case "cyberPolicy", "misalignmentPolicyViolation":
		return harness.ErrRefused
	case "contextWindowExceeded":
		return harness.ErrContextOverflow
	case "modelNotFound":
		return harness.ErrModelNotFound
	case "serverOverloaded", "internalServerError", "httpConnectionFailed", "responseStreamConnectionFailed",
		"responseStreamDisconnected", "responseTooManyFailedAttempts":
		return harness.ErrProviderUnreachable
	}
	return harness.ClassifyError(e.Message)
}

// errorText is the notice line: Codex's own message, or a sentence for the
// reason when it sent none.
func errorText(e turnError, reason string) string {
	if e.Message != "" {
		return e.Message
	}
	if m := e.Misalignment; m != nil && m.DetailedExplanation != nil && *m.DetailedExplanation != "" {
		return *m.DetailedExplanation
	}
	switch reason {
	case harness.ErrAuth:
		return "Codex isn't signed in or its login expired."
	case harness.ErrRateLimit:
		return "Codex hit a usage or rate limit."
	case harness.ErrContextOverflow:
		return "The conversation is too long for the model's context window."
	case harness.ErrModelNotFound:
		return "The selected model doesn't exist or this account can't use it."
	case harness.ErrProviderUnreachable:
		return "Codex can't reach the model provider."
	case harness.ErrRefused:
		return "Codex's safety policy blocked this request."
	}
	return "The turn failed."
}

// errorItem is the notice for a turn's error. A final error and the failed
// turn/completed that follows share one id, so the chat shows it once; a
// retried one keeps one warning per turn that counts the attempts.
func errorItem(e turnError, turn string, retry bool, attempt int, at time.Time) harness.Item {
	reason := errorReason(e)
	text := errorText(e, reason)
	if retry {
		body := harness.ErrorNotice(reason, fmt.Sprintf("%s (retrying, attempt %d)", text, attempt))
		body.Level = "warning"
		return harness.Item{ID: "retry-" + turn, TurnID: turn, Kind: harness.KindNotice, At: at, Notice: body}
	}
	return harness.Item{ID: "error-" + turn, TurnID: turn, Kind: harness.KindNotice, At: at, Notice: harness.ErrorNotice(reason, text)}
}
