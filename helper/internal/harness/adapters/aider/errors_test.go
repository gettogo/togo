package aider

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// mapFile maps a history fixture as one record, closes the last turn by
// idle, and describes the latest version of each item.
func mapFile(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	h := newHistory(harness.Env{Cwd: t.TempDir(), Argv: []string{"/Users/me/.local/bin/aider"}, Vars: map[string]string{}})
	items, _ := h.Map(record(t, strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")))
	items = append(items, h.CloseIdle(time.Date(2026, 11, 7, 23, 40, 0, 0, time.UTC))...)
	var desc []string
	for _, it := range latest(items) {
		switch it.Kind {
		case harness.KindUser:
			desc = append(desc, "user:"+it.User.Text)
		case harness.KindAssistant:
			desc = append(desc, "assistant:"+it.Assistant.Text)
		case harness.KindTool:
			desc = append(desc, "tool:"+it.Tool.Title)
		case harness.KindNotice:
			desc = append(desc, it.Notice.Level+":"+it.Notice.Code+":"+it.Notice.Reason+":"+it.Notice.Text)
		case harness.KindTurn:
			desc = append(desc, "turn:"+it.Turn.State)
		}
	}
	return desc
}

func expect(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("items:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Real: aider 0.86.2 with an empty HOME and no keys stops at its OpenRouter
// sign-in question before any prompt; a typed message is rejected as an
// answer. The launch command line is dropped. The question itself is the
// prompt detector's (Needs you); the chat says why aider is not working.
func TestHistoryOnboarding(t *testing.T) {
	expect(t, mapFile(t, "real-onboarding.history.md"), []string{
		"error:error:auth:Aider has no model or API key set up. It is asking to sign in to OpenRouter.",
		"error:error:refused:Aider is waiting for an answer to its question, so it didn't take that as a message.",
	})
}

// Real: an unreachable base URL. Aider retries with backoff: one notice
// that says it retries, until the last failure with no retry after it,
// which fails the turn. The startup model warning is a warning notice, not
// a step, and its follow-up lines and answered URL offer are dropped.
func TestHistoryConnectRetry(t *testing.T) {
	expect(t, mapFile(t, "real-connect-retry.history.md"), []string{
		"tool:Detected dumb terminal, disabling fancy input and pretty output.",
		"warning:error::Warning for openai/qwen3.8-27b: Unknown context window size and costs, using sane defaults.",
		"tool:Aider v0.86.2",
		"user:hi",
		"turn:failed",
		"error:error:provider_unreachable:Can't reach the provider: Connection error.",
	})
}

// While aider still retries, the turn runs and the notice is a warning.
func TestHistoryRetryKeepsTurnRunning(t *testing.T) {
	h := newHistory(harness.Env{Cwd: t.TempDir(), Vars: map[string]string{}})
	h.Map(record(t, []string{"# aider chat started at 2026-11-07 22:32:30", "", "#### hi  ",
		"> litellm.InternalServerError: InternalServerError: OpenAIException - Connection error.  "}))
	items, _ := h.Map(record(t, []string{"> The API provider's servers are down or overloaded.  ", "> Retrying in 0.2 seconds...  "}))
	if len(items) != 2 || items[0].Notice == nil || items[0].Notice.Level != "warning" ||
		items[0].Notice.Text != "Can't reach the provider: Connection error. Retrying…" {
		t.Fatalf("items = %+v", items)
	}
	if h.failed {
		t.Fatal("a retried error failed the turn")
	}
	h.Map(record(t, []string{"> litellm.InternalServerError: InternalServerError: OpenAIException - Connection error.  ", "> Retrying in 0.5 seconds...  "}))
	if h.notice.Notice.Text != "Can't reach the provider: Connection error. Retrying (attempt 2)…" {
		t.Fatalf("text = %q", h.notice.Notice.Text)
	}
}

// Real: a model the server does not route.
func TestHistoryModelNotFound(t *testing.T) {
	expect(t, mapFile(t, "real-model-not-found.history.md"), []string{
		"tool:Aider v0.86.2",
		"user:hi",
		"turn:failed",
		"error:error:other:The provider rejected the request: no router for requested model",
	})
}

// Written from the formats in aider 0.86's source (models.py
// sanity_check_model, base_coder.check_tokens, send_message's traceback,
// the ^C warning): a missing key, context overflow, a crash, an interrupt.
func TestHistorySourceShapes(t *testing.T) {
	expect(t, mapFile(t, "source-limits.history.md"), []string{
		"warning:error::Warning: openai/gpt-4o expects these environment variables. Not set: OPENAI_API_KEY",
		"user:summarise the repo",
		"turn:failed",
		"error:error:context_overflow:The chat is too long for the model. Use /drop or /clear to make room.",
		"user:try again",
		"turn:failed",
		"error:error:crashed:Aider hit an internal error: ValueError: bad response",
		"user:and again",
		"turn:interrupted",
		"assistant:Thinking about it",
	})
}

func TestProviderError(t *testing.T) {
	for _, c := range []struct{ class, msg, reason string }{
		{"AuthenticationError", "Incorrect API key provided", harness.ErrAuth},
		{"RateLimitError", "429 Too Many Requests", harness.ErrRateLimit},
		{"NotFoundError", "model gpt-9 not found", harness.ErrModelNotFound},
		{"APIConnectionError", "Connection refused", harness.ErrProviderUnreachable},
		{"ServiceUnavailableError", "503", harness.ErrProviderUnreachable},
		{"BadRequestError", "maximum context length is 8192 tokens", harness.ErrContextOverflow},
	} {
		if r, text := providerError(c.class, c.msg); r != c.reason || !strings.HasSuffix(text, c.msg) {
			t.Errorf("%s: %s %q", c.class, r, text)
		}
	}
}
