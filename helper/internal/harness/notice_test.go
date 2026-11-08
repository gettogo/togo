package harness

import (
	"strings"
	"testing"
)

func TestClassifyError(t *testing.T) {
	cases := map[string]string{
		"Please answer with one of: yes, no, skip, all":          ErrRefused,
		"litellm.AuthenticationError: OpenrouterException - 401": ErrAuth,
		"Error: Invalid API key":                                 ErrAuth,
		"litellm.RateLimitError: 429 Too Many Requests":          ErrRateLimit,
		"This model's maximum context length is 8192 tokens":     ErrContextOverflow,
		"model 'qwen9' not found":                                ErrModelNotFound,
		"litellm.APIConnectionError: Connection refused":         ErrProviderUnreachable,
		"Traceback (most recent call last):":                     ErrCrashed,
		"Something odd happened":                                 ErrOther,
	}
	for text, want := range cases {
		if got := ClassifyError(text); got != want {
			t.Errorf("ClassifyError(%q) = %s, want %s", text, got, want)
		}
	}
}

func TestErrorNotice(t *testing.T) {
	n := ErrorNotice(ErrAuth, "  401 Unauthorized \n")
	if n.Level != "error" || n.Code != NoticeError || n.Reason != ErrAuth || n.Text != "401 Unauthorized" {
		t.Fatalf("auth notice = %+v", n)
	}
	if n := ErrorNotice(ErrRateLimit, "429"); n.Code != NoticeRateLimit || n.Level != "warning" {
		t.Fatalf("rate limit notice = %+v", n)
	}
	if n := ErrorNotice("bogus", "x"); n.Reason != ErrOther {
		t.Fatalf("unknown reason = %+v", n)
	}
	if n := ErrorNotice(ErrOther, strings.Repeat("é", 900)); len([]rune(n.Text)) != MaxNoticeText {
		t.Fatalf("text not capped: %d", len([]rune(n.Text)))
	}
}
