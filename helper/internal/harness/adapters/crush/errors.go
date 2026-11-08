package crush

import (
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// errorNotice is the notice for a message Crush finished with reason
// "error". Crush (agent.go, 0.100) stores the provider error's title as
// the finish message ("Provider Error", "Not Found", "Unauthorized",
// "Too Many Requests") and its text as details, for example:
//
//	{"reason":"error","message":"Provider Error","details":"retry error: Post \"http://…/v1/chat/completions\": dial tcp …: connect: connection refused"}
//	{"reason":"error","message":"Not Found","details":"no router for requested model"}
func errorNotice(title, details string) *harness.NoticeBody {
	detail := cleanDetail(details)
	all := title + ": " + detail
	reason := harness.ClassifyError(all)
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "unauthorized", "forbidden":
		reason = harness.ErrAuth
	case "too many requests":
		reason = harness.ErrRateLimit
	case "not found":
		if reason == harness.ErrOther {
			reason = harness.ErrModelNotFound
		}
	}
	return harness.ErrorNotice(reason, sentence(reason, title, detail))
}

// sentence is the short line the chat shows for reason, with the
// provider's own words after it.
func sentence(reason, title, detail string) string {
	lead := map[string]string{
		harness.ErrAuth:                "The provider refused the API key",
		harness.ErrProviderUnreachable: "Can't reach the provider",
		harness.ErrRateLimit:           "The provider's rate limit or quota was hit",
		harness.ErrModelNotFound:       "The provider doesn't have this model",
		harness.ErrContextOverflow:     "The conversation is too long for the model",
	}[reason]
	if lead == "" {
		lead = strings.TrimSpace(title)
		if lead == "" || strings.EqualFold(lead, "Provider Error") {
			lead = "The provider returned an error"
		}
	}
	if detail == "" {
		return lead + "."
	}
	return lead + ": " + detail
}

// postURL is the `Post "url": ` an http client error starts with.
var postURL = regexp.MustCompile(`^(?i:(get|post)) "[^"]*": `)

// cleanDetail drops the retry wrapper and request line Go's http client
// puts in front of the error that matters.
func cleanDetail(s string) string {
	s = strings.TrimSpace(s)
	for {
		t := strings.TrimPrefix(s, "retry error: ")
		t = strings.TrimPrefix(t, "failed to start agent processing stream: ")
		t = postURL.ReplaceAllString(t, "")
		if t == s {
			return s
		}
		s = t
	}
}
