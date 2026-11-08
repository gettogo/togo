// Package errtext builds the error notices adapters show for a failed turn
// on top of harness.ErrorNotice: the reason, and text that says what went
// wrong in plain words before the agent's own detail ("401 Incorrect API
// key provided: …", "404 no router for requested model"), so nothing the
// agent said is lost.
package errtext

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

var (
	leadStatus = regexp.MustCompile(`^\s*(?:Error:\s*)?(?:HTTP\s*)?([1-5]\d\d)\b`)
	anyStatus  = regexp.MustCompile(`\b(?:status(?: code)?|HTTP)[ :=]*([45]\d\d)\b`)
	// Words harness.ClassifyError does not know, seen in the agents'
	// own errors: pi/omp against llama.cpp, Node's fetch.
	modelWords   = regexp.MustCompile(`(?i)no router for requested model|model_not_found`)
	networkWords = regexp.MustCompile(`(?i)fetch failed|socket hang up|getaddrinfo|econnreset`)
)

// StatusOf finds the HTTP status an error text starts with or names
// ("status 429", "HTTP 503"), or 0.
func StatusOf(raw string) int {
	m := leadStatus.FindStringSubmatch(raw)
	if m == nil {
		m = anyStatus.FindStringSubmatch(raw)
	}
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// Classify picks the harness reason for an error from its HTTP status (0
// when unknown) and words.
func Classify(raw string, status int) string {
	if status == 0 {
		status = StatusOf(raw)
	}
	if r := harness.ClassifyError(raw); r != harness.ErrOther && r != harness.ErrRefused && r != harness.ErrCrashed {
		return r
	}
	switch {
	case status == 401 || status == 403:
		return harness.ErrAuth
	case status == 429:
		return harness.ErrRateLimit
	case status == 404 || modelWords.MatchString(raw):
		return harness.ErrModelNotFound
	case status == 408 || status >= 500 || networkWords.MatchString(raw):
		return harness.ErrProviderUnreachable
	}
	return harness.ErrOther
}

var leads = map[string]string{
	harness.ErrAuth:                "The provider rejected the sign-in or API key",
	harness.ErrRateLimit:           "Rate limited or out of credits",
	harness.ErrModelNotFound:       "The model was not found",
	harness.ErrContextOverflow:     "The conversation is too long for the model",
	harness.ErrProviderUnreachable: "Could not reach the provider",
}

// Notice is the notice body for an error the agent recorded as text: the
// reason Classify picks, its plain lead, and raw's first line. fallback is
// the text when raw is empty.
func Notice(raw string, status int, fallback string) *harness.NoticeBody {
	if strings.TrimSpace(raw) == "" {
		return harness.ErrorNotice(harness.ErrOther, fallback)
	}
	return Of(Classify(raw, status), raw)
}

// Of is the notice body for an error whose reason the agent named itself.
func Of(reason, detail string) *harness.NoticeBody {
	return With(reason, leads[reason], detail)
}

// With is the notice body for reason with an explicit lead; lead and
// detail may each be empty.
func With(reason, lead, detail string) *harness.NoticeBody {
	return harness.ErrorNotice(reason, join(lead, detail))
}

// join is lead, then detail's first line.
func join(lead, detail string) string {
	detail = strings.TrimSpace(detail)
	if i := strings.IndexByte(detail, '\n'); i >= 0 {
		detail = strings.TrimSpace(detail[:i])
	}
	switch {
	case lead == "":
		return detail
	case detail == "":
		return lead
	}
	return lead + ": " + detail
}
