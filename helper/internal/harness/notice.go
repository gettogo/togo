package harness

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Error notices: the one shape every adapter uses to tell the chat that the
// agent hit an error (a refused key, an unreachable provider, a crash), so
// the app draws them alike and can say what to do.
//
// Adapters call ErrorNotice with a reason and the agent's own words:
//
//	it := harness.Item{ID: id, Kind: harness.KindNotice, At: now,
//		Notice: harness.ErrorNotice(harness.ErrAuth, "OpenRouter: 401 Unauthorized")}
//
// When only the text is known, ClassifyError picks the reason from it:
//
//	harness.ErrorNotice(harness.ClassifyError(line), line)
//
// A rate limit keeps its own code (rate_limit, level warning) so older apps
// still draw it as a wait; every other reason is code error, level error.
// Reason is new in protocol v1 and optional, so older apps ignore it.

// Error notice reasons.
const (
	ErrAuth                = "auth"                 // sign-in missing, key rejected (401/403)
	ErrProviderUnreachable = "provider_unreachable" // connection refused, DNS, 5xx, timeout
	ErrRateLimit           = "rate_limit"           // 429, quota
	ErrModelNotFound       = "model_not_found"      // unknown model name
	ErrContextOverflow     = "context_overflow"     // prompt too long for the model
	ErrCrashed             = "crashed"              // the agent died: traceback, panic, non-zero exit
	ErrRefused             = "refused"              // the agent refused what was typed ("Please answer with one of")
	ErrOther               = "other"
)

// NoticeEnded is the code of the notice that says the agent process exited.
const NoticeEnded = "ended"

// MaxNoticeText bounds an error notice's text, in runes.
const MaxNoticeText = 500

// ErrorNotice returns the body of an error notice for reason (one of the
// Err* constants; anything else is ErrOther) with the agent's text, trimmed
// and capped at MaxNoticeText runes.
func ErrorNotice(reason, text string) *NoticeBody {
	switch reason {
	case ErrAuth, ErrProviderUnreachable, ErrRateLimit, ErrModelNotFound, ErrContextOverflow, ErrCrashed, ErrRefused, ErrOther:
	default:
		reason = ErrOther
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > MaxNoticeText {
		text = string([]rune(text)[:MaxNoticeText-1]) + "…"
	}
	if reason == ErrRateLimit {
		return &NoticeBody{Level: "warning", Code: NoticeRateLimit, Reason: reason, Text: text}
	}
	return &NoticeBody{Level: "error", Code: NoticeError, Reason: reason, Text: text}
}

// errorClasses map an error's words to a reason, first match wins: an
// overflow says "too many tokens", which is no rate limit; a 401 from an
// unreachable-looking URL is still auth.
var errorClasses = []struct {
	reason string
	re     *regexp.Regexp
}{
	{ErrRefused, regexp.MustCompile(`(?i)\bplease (answer|enter|type|choose|select) (with )?(one of|a valid|y|yes)\b|\binvalid (choice|option|selection|answer)\b`)},
	{ErrContextOverflow, regexp.MustCompile(`(?i)context (length|window|limit)|maximum context|too many tokens|prompt is too long|token limit|exceeds? the (model'?s? )?(context|max)`)},
	{ErrAuth, regexp.MustCompile(`(?i)\b(401|403)\b|unauthori[sz]ed|forbidden|authenticat|invalid (api[ _-]?)?key|api[ _-]?key|not logged in|log ?in required|credentials?\b|permission denied by provider`)},
	{ErrRateLimit, regexp.MustCompile(`(?i)\b429\b|rate[ _-]?limit|too many requests|quota|overloaded`)},
	{ErrModelNotFound, regexp.MustCompile(`(?i)model\b.{0,60}\b(not found|does not exist|doesn'?t exist|unknown|not available|unsupported)|(unknown|invalid|no such) model|not ?found.{0,20}model`)},
	{ErrProviderUnreachable, regexp.MustCompile(`(?i)connection (refused|reset|error|failed)|could not connect|failed to connect|cannot connect|econnrefused|enotfound|timed? ?out|unreachable|no route to host|name resolution|nodename nor servname|\b(502|503|504)\b|bad gateway|service unavailable|network error|apiconnectionerror`)},
	{ErrCrashed, regexp.MustCompile(`(?i)traceback \(most recent call last\)|^panic:|segmentation fault|\bfatal error\b|exit (status|code) [1-9]|exited with (code|status) [1-9]|uncaught exception|unhandled (exception|rejection)`)},
}

// ClassifyError picks the reason an error text names, ErrOther when none.
func ClassifyError(text string) string {
	for _, c := range errorClasses {
		if c.re.MatchString(text) {
			return c.reason
		}
	}
	return ErrOther
}
