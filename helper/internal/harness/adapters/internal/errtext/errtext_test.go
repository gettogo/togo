package errtext

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func TestNotice(t *testing.T) {
	cases := []struct {
		raw    string
		status int
		reason string
		text   string
	}{
		// Real records: pi and omp with a bogus key and an unknown model.
		{"401 Incorrect API key provided: sk-bogus000.", 0, harness.ErrAuth,
			"The provider rejected the sign-in or API key: 401 Incorrect API key provided: sk-bogus000."},
		{"404 no router for requested model", 0, harness.ErrModelNotFound, "The model was not found: 404 no router for requested model"},
		{"429 Rate limit reached for requests", 0, harness.ErrRateLimit, "Rate limited or out of credits: 429 Rate limit reached for requests"},
		{"This model's maximum context length is 128000 tokens", 0, harness.ErrContextOverflow,
			"The conversation is too long for the model: This model's maximum context length is 128000 tokens"},
		{"fetch failed", 0, harness.ErrProviderUnreachable, "Could not reach the provider: fetch failed"},
		{"boom", 503, harness.ErrProviderUnreachable, "Could not reach the provider: boom"},
		{"denied", 403, harness.ErrAuth, "The provider rejected the sign-in or API key: denied"},
		{"something odd\nstack", 0, harness.ErrOther, "something odd"},
		{"", 0, harness.ErrOther, "fallback"},
	}
	for _, c := range cases {
		n := Notice(c.raw, c.status, "fallback")
		if n.Reason != c.reason || n.Text != c.text {
			t.Errorf("Notice(%q) = %s %q, want %s %q", c.raw, n.Reason, n.Text, c.reason, c.text)
		}
	}
}

func TestStatusOf(t *testing.T) {
	for raw, want := range map[string]int{"401 nope": 401, "Error: 429 slow": 429, "request failed with status code 503": 503, "fine": 0, "took 200ms": 0} {
		if got := StatusOf(raw); got != want {
			t.Errorf("StatusOf(%q) = %d, want %d", raw, got, want)
		}
	}
}
