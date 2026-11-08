package aider

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// What aider writes to the chat history when something goes wrong. Its
// tool_error and tool_warning lines are blockquotes like any other tool
// output (io._tool_message, 0.86), so they are told apart by their words:
// the litellm exception aider prints (base_coder.send_message), the
// friendly description it adds from exceptions.py, "Retrying in N
// seconds..." while it retries, the token-limit report, the onboarding
// lines before its OpenRouter sign-in question, and its "Warning…" lines.

// lineKind is what one tool line of the history is.
type lineKind int

const (
	lineOther     lineKind = iota // a tool step
	lineSkip                      // noise: dropped
	lineError                     // an error notice
	lineWarning                   // a warning notice
	lineRetry                     // aider retries the request that just failed
	lineInterrupt                 // the user pressed ^C
)

// classified is one line's meaning.
type classified struct {
	kind   lineKind
	reason string // for lineError
	text   string
	fails  bool   // the error ends the turn as failed
	absorb absorb // how the lines after it read
}

// absorb says which lines after an error or warning belong to it.
type absorb int

const (
	absorbNone      absorb = iota
	absorbWarning          // "- KEY: Not set", "Did you mean one of these?"
	absorbTokens           // the token-limit report and its advice
	absorbTraceback        // a Python traceback up to the exception line
)

var (
	litellmLine  = regexp.MustCompile(`^litellm\.(\w+):\s*(.*)$`)
	sdkPrefix    = regexp.MustCompile(`^(\w+Error:\s*)?(\w+Exception\s*-\s*)?`)
	retryLine    = regexp.MustCompile(`^Retrying in [\d.]+ seconds\.\.\.$`)
	providerLine = regexp.MustCompile(`^The API provider\b`)
	urlOffer     = regexp.MustCompile(`^Open (documentation url|URL) for more info\?`)
	aiderURL     = regexp.MustCompile(`^https?://aider\.chat/\S*$`)
	skipCheck    = regexp.MustCompile(`^You can skip this check with --`)
	warningLine  = regexp.MustCompile(`^(Warning\b|Update git (name|email) with:|Skipping .+ that (matches|is not)|Dropping .+ from the chat\.|Can not create |Only \d+ reflections allowed|Empty response received from LLM)`)
	envLine      = regexp.MustCompile(`^- (\w+): (Not set|Set)$`)
	tokenLimit   = regexp.MustCompile(`^(Model .+ has hit a token limit!|Your estimated chat context of .+ exceeds the .+ token limit)`)
	tokenReport  = regexp.MustCompile(`^(Token counts below are approximate\.|(Input|Output|Total) tokens: ~|To reduce |- |For more info: )`)
	exceptLine   = regexp.MustCompile(`^[A-Za-z_][\w.]*(Error|Exception|Interrupt)\b:?`)
	answerLine   = regexp.MustCompile(`^Please answer with one of: `)
	noModelLine  = regexp.MustCompile(`^(No LLM model was specified and no API keys were provided\.|The specified model '.+' requires an OpenRouter API key)`)
	routerBlurb  = regexp.MustCompile(`^OpenRouter provides free and paid access`)
	signInFailed = regexp.MustCompile(`^(OpenRouter authentication did not complete successfully|Authentication with OpenRouter failed|Authentication failed: |Temporary authentication server failed)`)
	formatLine   = regexp.MustCompile(`^The LLM did not conform to the edit format\.`)
	commitFailed = regexp.MustCompile(`^Unable to commit: `)
)

// classify reads one tool line.
func (h *history) classify(l string) classified {
	switch {
	case l == "" || strings.HasPrefix(l, "Tokens: "):
		return classified{kind: lineSkip}
	case !h.launchSeen && h.isLaunch(l):
		h.launchSeen = true
		return classified{kind: lineSkip}
	case retryLine.MatchString(l):
		return classified{kind: lineRetry}
	case providerLine.MatchString(l), urlOffer.MatchString(l), aiderURL.MatchString(l), skipCheck.MatchString(l), routerBlurb.MatchString(l):
		return classified{kind: lineSkip}
	case l == "^C KeyboardInterrupt":
		return classified{kind: lineInterrupt}
	case l == "^C again to exit":
		return classified{kind: lineSkip}
	case answerLine.MatchString(l):
		return classified{kind: lineError, reason: harness.ErrRefused,
			text: "Aider is waiting for an answer to its question, so it didn't take that as a message."}
	case noModelLine.MatchString(l):
		return classified{kind: lineError, reason: harness.ErrAuth,
			text: "Aider has no model or API key set up. It is asking to sign in to OpenRouter."}
	case signInFailed.MatchString(l):
		return classified{kind: lineError, reason: harness.ErrAuth, text: "Signing in to OpenRouter failed.", fails: true}
	case tokenLimit.MatchString(l):
		return classified{kind: lineError, reason: harness.ErrContextOverflow, fails: true, absorb: absorbTokens,
			text: "The chat is too long for the model. Use /drop or /clear to make room."}
	case formatLine.MatchString(l):
		return classified{kind: lineError, reason: harness.ErrOther, text: "The model's reply didn't follow aider's edit format."}
	case commitFailed.MatchString(l):
		return classified{kind: lineError, reason: harness.ErrOther, text: l}
	case litellmLine.MatchString(l):
		m := litellmLine.FindStringSubmatch(l)
		reason, text := providerError(m[1], sdkPrefix.ReplaceAllString(m[2], ""))
		return classified{kind: lineError, reason: reason, text: text, fails: true}
	case strings.HasPrefix(l, "Traceback "):
		return classified{kind: lineError, reason: harness.ErrCrashed, text: "Aider hit an internal error.", fails: true, absorb: absorbTraceback}
	case errorLine.MatchString(l):
		return classified{kind: lineError, reason: harness.ClassifyError(l), text: l, fails: true}
	case warningLine.MatchString(l):
		return classified{kind: lineWarning, text: l, absorb: absorbWarning}
	}
	return classified{kind: lineOther}
}

// follows reports whether l belongs to the error or warning before it, and
// updates that notice's text when it adds to it.
func (h *history) follows(a absorb, l string, notice *harness.Item) bool {
	switch a {
	case absorbWarning:
		if m := envLine.FindStringSubmatch(l); m != nil {
			if m[2] == "Not set" && notice != nil {
				notice.Notice.Text = strings.TrimSuffix(notice.Notice.Text, ".") + ". Not set: " + m[1]
			}
			return true
		}
		return l == "Did you mean one of these?" || strings.HasPrefix(l, "- ")
	case absorbTokens:
		return tokenReport.MatchString(l)
	case absorbTraceback:
		if exceptLine.MatchString(l) && notice != nil {
			notice.Notice.Text = "Aider hit an internal error: " + l
		}
		return true
	}
	return false
}

// isLaunch reports whether l is the command line aider logs first thing
// after its start header (main.py: tool_output(cmd_line, log_only=True)).
func (h *history) isLaunch(l string) bool {
	f := strings.Fields(l)
	if len(f) == 0 {
		return false
	}
	base := filepath.Base(f[0])
	if base == "aider" || strings.HasPrefix(base, "aider-") || strings.HasSuffix(f[0], "/aider/__main__.py") {
		return true
	}
	return len(h.env.Argv) > 0 && f[0] == h.env.Argv[0] && strings.ContainsRune(f[0], '/')
}

// providerError turns a litellm exception class and message into a reason
// and a short sentence (exceptions.py lists the classes aider handles).
func providerError(class, msg string) (string, string) {
	msg = strings.TrimSpace(msg)
	say := func(reason, sentence string) (string, string) {
		if msg == "" {
			return reason, sentence + "."
		}
		return reason, sentence + ": " + msg
	}
	switch class {
	case "AuthenticationError":
		return say(harness.ErrAuth, "The provider rejected the API key")
	case "PermissionDeniedError":
		return say(harness.ErrAuth, "The provider denied access")
	case "RateLimitError":
		return say(harness.ErrRateLimit, "The provider's rate limit or quota was hit")
	case "BudgetExceededError":
		return say(harness.ErrRateLimit, "The spending budget is used up")
	case "NotFoundError":
		return say(harness.ErrModelNotFound, "The provider doesn't have this model")
	case "ContextWindowExceededError":
		return say(harness.ErrContextOverflow, "The chat is too long for the model")
	case "APIConnectionError", "Timeout":
		return say(harness.ErrProviderUnreachable, "Can't reach the provider")
	case "InternalServerError", "ServiceUnavailableError", "BadGatewayError":
		if strings.Contains(strings.ToLower(msg), "connection") {
			return say(harness.ErrProviderUnreachable, "Can't reach the provider")
		}
		return say(harness.ErrProviderUnreachable, "The provider's servers are down or overloaded")
	case "ContentPolicyViolationError":
		return say(harness.ErrOther, "The provider refused the request under its content policy")
	case "BadRequestError":
		if r := harness.ClassifyError(msg); r != harness.ErrOther {
			return say(r, "The provider rejected the request")
		}
		return say(harness.ErrOther, "The provider rejected the request")
	}
	switch r := harness.ClassifyError(class + " " + msg); r {
	case harness.ErrAuth:
		return say(r, "The provider rejected the sign-in")
	case harness.ErrRateLimit:
		return say(r, "The provider's rate limit or quota was hit")
	case harness.ErrModelNotFound:
		return say(r, "The provider doesn't have this model")
	case harness.ErrContextOverflow:
		return say(r, "The chat is too long for the model")
	case harness.ErrProviderUnreachable:
		return say(r, "Can't reach the provider")
	default:
		return say(r, "The provider returned an error")
	}
}
