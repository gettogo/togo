package session

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

// statusAwaitingApproval is the status of a session waiting on an approval.
const statusAwaitingApproval = "needs_you"

// ReplyRefusedError means a reply was not typed because the pane is not an
// agent waiting for input. Reason is the protocol's -32006 data.reason:
// wire.ConflictShell, wire.ConflictNeedsYou or wire.ConflictEnded.
type ReplyRefusedError struct {
	Reason string
}

// Error names the reason.
func (e *ReplyRefusedError) Error() string { return "reply refused: " + e.Reason }

// ValidateSendText checks reply text against the protocol: 1 to
// wire.MaxSendText Unicode scalar values of valid UTF-8 with no control
// character other than newline. That keeps out escape sequences, including
// the end of a bracketed paste, carriage returns and tabs. A rejection is
// an *InvalidParamsError with Reason wire.ReasonText; its message never
// quotes the text.
func ValidateSendText(text string) error {
	bad := func(msg string) error { return &InvalidParamsError{Reason: wire.ReasonText, Msg: msg} }
	switch n := utf8.RuneCountInString(text); {
	case text == "":
		return bad("text must not be empty")
	case !utf8.ValidString(text):
		return bad("text is not valid UTF-8")
	case n > wire.MaxSendText:
		return bad(fmt.Sprintf("text has %d characters, the limit is %d", n, wire.MaxSendText))
	case strings.ContainsFunc(text, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }):
		return bad("text contains a control character")
	}
	return nil
}

// SendText types text into a managed session's pane as one bracketed paste
// and, when submit is set, presses Enter after it. status and exit are the
// session's status as the app sees it (the hub's view); an empty status
// falls back to what tmux reports. Nothing is typed, and the error is a
// *ReplyRefusedError, when the session runs the plain shell, waits on an
// approval, has ended or exited, failed with an exit code, or has a
// shell in the foreground of its pane, where the text would run as
// a command. The text is never logged or put in an error.
func (s *Service) SendText(ctx context.Context, id, text string, submit bool, status string, exit *wire.SessionExit) error {
	if err := requireManaged(id); err != nil {
		return err
	}
	if err := ValidateSendText(text); err != nil {
		return err
	}
	// One reply at a time per session: the checks, the paste, the settle
	// and Enter of two replies must not interleave into one submission.
	mu, _ := replyLocks.LoadOrStore(id, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	sess, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if reason := replyRefusal(sess.Agent, cmp.Or(status, sess.Status), exit); reason != "" {
		return &ReplyRefusedError{Reason: reason}
	}
	if sess.Status == statusExited {
		return &ReplyRefusedError{Reason: wire.ConflictEnded}
	}
	target := "=" + id + ":"
	cmd, err := s.Tmux.PaneCommand(ctx, target)
	if err != nil {
		return notFoundOr(err, id)
	}
	if isShell(cmd, s.shell()) {
		return &ReplyRefusedError{Reason: wire.ConflictEnded}
	}
	if err := typeReply(ctx, s.Tmux, id, text, submit, sleepCtx); err != nil {
		return notFoundOr(err, id)
	}
	return nil
}

// replyLocks holds a *sync.Mutex per session id that has been replied to.
// Ids are random and never reused, so an entry is a few bytes per session.
var replyLocks sync.Map

// knownShells are the shells whose name in the foreground of an agent's
// pane means the agent is gone and the text would run as a command.
var knownShells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true,
	"mksh": true, "tcsh": true, "csh": true, "nu": true, "pwsh": true,
}

// isShell reports whether cmd, a pane's foreground command, is a shell:
// a known one or the login shell. A login shell reports a leading "-". The
// list matters on macOS, where /bin/sh runs bash and reports as bash.
func isShell(cmd, loginShell string) bool {
	cmd = strings.TrimPrefix(cmd, "-")
	return knownShells[cmd] || cmd == filepath.Base(loginShell)
}

// replyPane is the part of the tmux client a reply uses.
type replyPane interface {
	PasteText(ctx context.Context, target, text string) error
	SendKeys(ctx context.Context, target string, literal bool, keys ...string) error
	CaptureHistory(ctx context.Context, name string, lines int, join bool) (string, error)
}

// How long typeReply lets the pane settle between the paste and Enter.
// A TUI reading the end of a paste and Enter in one burst can take the
// Enter as part of the paste and leave the reply unsent.
const (
	settleMin      = 120 * time.Millisecond
	settleMax      = 500 * time.Millisecond
	settleInterval = 50 * time.Millisecond
)

// typeReply pastes text into session id and, with submit, presses Enter
// once the pane has settled: its screen stayed the same for one interval
// or shows the last line of the text, after at least settleMin and at
// most settleMax.
func typeReply(ctx context.Context, p replyPane, id, text string, submit bool, sleep func(context.Context, time.Duration) error) error {
	target := "=" + id + ":"
	if err := p.PasteText(ctx, target, text); err != nil {
		return err
	}
	if !submit {
		return nil
	}
	if err := settle(ctx, p, id, text, sleep); err != nil {
		return err
	}
	return p.SendKeys(ctx, target, false, "Enter")
}

func settle(ctx context.Context, p replyPane, id, text string, sleep func(context.Context, time.Duration) error) error {
	last := strings.TrimSpace(text[strings.LastIndexByte(text, '\n')+1:])
	prev, prevErr := p.CaptureHistory(ctx, id, 0, true)
	for waited := time.Duration(0); waited < settleMax; {
		if err := sleep(ctx, settleInterval); err != nil {
			return err
		}
		waited += settleInterval
		cur, err := p.CaptureHistory(ctx, id, 0, true)
		calm := err == nil && prevErr == nil && (cur == prev || last != "" && strings.Contains(cur, last))
		if waited >= settleMin && (calm || err != nil) {
			return nil
		}
		prev, prevErr = cur, err
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// replyRefusal returns the reason a reply cannot go to a session running
// agent with the given status and exit, or "" when it can. A failure with
// no exit code (a usage limit or an error the agent reported) leaves the
// agent at its prompt, so it is not a refusal.
func replyRefusal(agent, status string, exit *wire.SessionExit) string {
	switch {
	case agent == "shell":
		return wire.ConflictShell
	case status == statusAwaitingApproval:
		return wire.ConflictNeedsYou
	case status == wire.StatusEnded || status == statusExited:
		return wire.ConflictEnded
	case status == wire.StatusFailed && exit != nil && exit.Code != nil:
		return wire.ConflictEnded
	}
	return ""
}
