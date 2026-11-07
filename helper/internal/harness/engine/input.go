package engine

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Typed text is checked against the harness's input line when the adapter
// declares one (spec.Input). Seen live on omp: a typed "/login anthropic"
// sat in the input line because a completion popup took the Enter, and the
// next prompt was appended to it ("/login anthropic/model"). So:
//
//   - before typing, leftover text in the line is cleared (the adapter's
//     clear keys, then one backspace per character), or nothing is typed;
//   - after Enter, the line must stop holding the text. When it does not,
//     the adapter's dismiss keys close the popup and Enter is pressed
//     again, a bounded number of times; then the send fails, with the text
//     left visible, instead of reporting it sent.

var (
	// submitWait is how long one Enter has to show on screen.
	submitWait = 2 * time.Second
	// submitRetries is how many more Enters follow the first.
	submitRetries = 2
)

// maxBackspaces bounds the backspaces one clear sends.
const maxBackspaces = 400

var ansiCSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// readInput returns the text in the input line and whether the line was
// found on screen.
func (a *adapter) readInput(screen string) (string, bool) {
	in := a.Input
	if in == nil {
		return "", false
	}
	re, err := regexp.Compile(in.Line)
	if err != nil {
		return "", false
	}
	lines := strings.Split(ansiCSI.ReplaceAllString(screen, ""), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		m := re.FindStringSubmatch(strings.TrimRight(lines[i], " \t\r"))
		if m == nil {
			continue
		}
		text := strings.TrimSpace(m[1])
		for _, e := range in.Empty {
			if ok, _ := regexp.MatchString(e, text); ok {
				return "", true
			}
		}
		return text, true
	}
	return "", false
}

// inputNow reads the session's input line.
func (e *Engine) inputNow(ctx context.Context, s *sess) (string, bool) {
	screen, err := e.cfg.Screen(ctx, s.id)
	if err != nil {
		return "", false
	}
	return s.ad.readInput(screen)
}

// clearInput empties an input line that holds leftover text. It returns a
// FallbackError when the text stays.
func (e *Engine) clearInput(ctx context.Context, s *sess) error {
	in := s.ad.Input
	if in == nil || e.cfg.Screen == nil {
		return nil
	}
	cur, ok := e.inputNow(ctx, s)
	if !ok || cur == "" {
		return nil
	}
	if len(in.Clear) > 0 {
		if err := e.cfg.Typist.Keys(ctx, s.id, in.Clear...); err != nil {
			return err
		}
		if !e.pause(ctx) {
			return ctx.Err()
		}
		if cur, ok = e.inputNow(ctx, s); !ok || cur == "" {
			return nil
		}
	}
	n := min(utf8.RuneCountInString(cur)+8, maxBackspaces)
	keys := make([]string, n)
	for i := range keys {
		keys[i] = "BSpace"
	}
	if err := e.cfg.Typist.Keys(ctx, s.id, keys...); err != nil {
		return err
	}
	if !e.pause(ctx) {
		return ctx.Err()
	}
	if cur, ok = e.inputNow(ctx, s); !ok || cur == "" {
		return nil
	}
	return &harness.FallbackError{Reason: "source",
		Detail: s.ad.Name + "'s input line already holds text (" + clipText(cur) + ") that could not be cleared. Nothing was typed."}
}

// confirmSubmit checks that Enter took text out of the input line, and
// presses it again after the dismiss keys when it did not.
func (e *Engine) confirmSubmit(ctx context.Context, s *sess, text string) error {
	in := s.ad.Input
	if in == nil || e.cfg.Screen == nil {
		return nil
	}
	for try := 0; ; try++ {
		if e.waitSubmitted(ctx, s, text) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if try >= submitRetries {
			return &harness.FallbackError{Reason: "source",
				Detail: s.ad.Name + " did not take Enter: “" + clipText(text) + "” is still in its input line. Check the terminal."}
		}
		if len(in.Dismiss) > 0 {
			if err := e.cfg.Typist.Keys(ctx, s.id, in.Dismiss...); err != nil {
				return err
			}
			if !e.pause(ctx) {
				return ctx.Err()
			}
		}
		if err := e.cfg.Typist.Keys(ctx, s.id, "Enter"); err != nil {
			return err
		}
	}
}

// waitSubmitted polls until the input line no longer holds text, or
// submitWait passes. A line that is gone (the harness opened a picker or a
// login box) counts as submitted.
func (e *Engine) waitSubmitted(ctx context.Context, s *sess, text string) bool {
	deadline := time.Now().Add(e.scaled(submitWait))
	for {
		cur, ok := e.inputNow(ctx, s)
		if !ok || !holds(cur, text) {
			return true
		}
		if time.Now().After(deadline) || !e.pause(ctx) {
			return false
		}
	}
}

// holds reports whether the input line's text is (part of) the typed
// text: a line that wraps shows only a piece of it.
func holds(line, text string) bool {
	line, text = squash(line), squash(text)
	if line == "" || text == "" {
		return false
	}
	piece := line
	if r := []rune(piece); len(r) > 30 {
		piece = string(r[:30])
	}
	return strings.Contains(text, piece) || strings.Contains(line, firstRunes(text, 30))
}

func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func clipText(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return s
}

// pause waits one verify poll; false when ctx ended.
func (e *Engine) pause(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(e.scaled(verifyPoll)):
		return true
	}
}

// typeChecked types text into an empty input line and, with submit,
// confirms Enter took it.
func (e *Engine) typeChecked(ctx context.Context, s *sess, text string, submit bool) error {
	if err := e.clearInput(ctx, s); err != nil {
		return err
	}
	if err := e.cfg.Typist.Type(ctx, s.id, text, submit, false); err != nil {
		return err
	}
	if !submit {
		return nil
	}
	return e.confirmSubmit(ctx, s, text)
}
