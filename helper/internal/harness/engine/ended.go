package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Errors a terminal-read agent prints, and the end of its process, become
// notices in the chat, so a turn that died says why instead of leaving the
// prompt alone under an endless Working.

// errorTail is how many non-empty lines at the bottom of the screen are
// read for an error: below it is only the agent's input line or prompt.
const errorTail = 12

// screenErrorLine matches a line an agent prints for an error: Python and
// LiteLLM exceptions, "Error: …", Go panics, and a refusal of what was
// typed. A line that merely mentions an error mid-sentence does not match.
var screenErrorLine = regexp.MustCompile(`(?i)^(\S*(error|exception)\b:?\s|error\b[:!]|fatal\b[:!]|panic:|traceback \(most recent call last\)|please answer with one of\b)`)

// screenError returns the lowest error line in the bottom of screen.
func screenError(screen string) (string, bool) {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < errorTail; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		seen++
		if screenErrorLine.MatchString(l) {
			return l, true
		}
	}
	return "", false
}

// screenNow reads the session's screen, "" when it cannot.
func (s *sess) screenNow() string {
	if s.e.cfg.Screen == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	screen, err := s.e.cfg.Screen(ctx, s.id)
	if err != nil {
		return ""
	}
	return screen
}

// exitNoticeLocked tells the chat, once, that the agent process exited,
// with its exit status when the hub knows it: ended for a clean exit or a
// stop, failed (crashed) for any other status.
func (s *sess) exitNoticeLocked() {
	if s.exitNoticed || !harness.Has(s.ad.Capabilities, harness.CapChat) {
		return
	}
	s.exitNoticed = true
	var x *wire.SessionExit
	if s.e.cfg.Exit != nil {
		x = s.e.cfg.Exit(s.id)
	}
	now := s.e.cfg.Now()
	it := harness.Item{ID: fmt.Sprintf("exit-%d", now.UnixMilli()), Kind: harness.KindNotice, At: now,
		Notice: exitNotice(s.ad.Name, s.hubStatus(), x)}
	s.publishLocked([]harness.Item{it}, nil)
}

// exitNotice words the end of the agent process.
func exitNotice(name, status string, x *wire.SessionExit) *harness.NoticeBody {
	code := -1
	if x != nil && x.Code != nil {
		code = *x.Code
	}
	switch {
	case x != nil && x.Reason == wire.ExitReasonInterrupted:
		return &harness.NoticeBody{Level: "info", Code: harness.NoticeEnded, Text: name + " was stopped."}
	case code == 0 || (code < 0 && status != wire.StatusFailed):
		return &harness.NoticeBody{Level: "info", Code: harness.NoticeEnded, Text: name + " exited."}
	case code > 0:
		n := harness.ErrorNotice(harness.ErrCrashed, fmt.Sprintf("%s quit with exit status %d.", name, code))
		return n
	}
	return harness.ErrorNotice(harness.ErrCrashed, name+" quit with an error.")
}
