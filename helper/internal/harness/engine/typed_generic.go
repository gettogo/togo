package engine

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Adapters that declare no input line (Pi, Copilot, Goose, Kimi,
// Droid, Auggie, Vibe, Aider, …) type slash commands into TUIs with the
// same completion popups as omp, whose popup once took the Enter and left
// the command unsent while the phone was told it was set. For them a typed
// command is checked by the screen row it shows on: the command is typed,
// the row holding it is found near the bottom, Enter is pressed, and that
// row must stop holding it. A row that holds another command instead is a
// completion that took the Enter: it is emptied and nothing is called
// sent.

// screenRows splits a capture into rows, escape sequences and odd spaces
// gone.
func screenRows(screen string) []string {
	rows := strings.Split(ansiCSI.ReplaceAllString(screen, ""), "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(plainSpaces(r), " \t\r")
	}
	return rows
}

// endsWith reports a row that ends with text, as an input line shows it
// (the cursor sits after it).
func endsWith(row, text string) bool {
	return strings.HasSuffix(squash(strings.Trim(row, "│┃| ")), squash(text))
}

// commandRow finds the lowest row that ends with text: the input line
// sits below any earlier echo of the same command.
func commandRow(rows []string, text string) int {
	for i := len(rows) - 1; i >= 0; i-- {
		if endsWith(rows[i], text) {
			return i
		}
	}
	return -1
}

// rowCommand is the slash command a row shows, if any: the text from its
// last " /" (or its start).
func rowCommand(row string) string {
	r := strings.TrimSpace(strings.Trim(row, "│┃| "))
	if i := strings.LastIndex(r, " /"); i >= 0 {
		r = r[i+1:]
	}
	// A command's name follows the slash at once: "/ commands" is a key
	// hint (Copilot CLI's footer: "· / commands   Claude Haiku 5.5").
	if !strings.HasPrefix(r, "/") || len(r) < 2 || r[1] == ' ' || r[1] == '\t' {
		return ""
	}
	return r
}

// genericSubmit types text without Enter, finds its row, presses Enter
// and checks the rows from there down let go of it, with one more Enter
// if they did not. A completion popup can sit under the input line, so
// the whole stretch to the bottom is read, not one row.
func (e *Engine) genericSubmit(ctx context.Context, s *sess, text string) error {
	if err := e.cfg.Typist.Type(ctx, s.id, text, false, false); err != nil {
		return err
	}
	row := -1
	deadline := time.Now().Add(e.scaled(submitWait))
	for {
		if screen, err := e.cfg.Screen(ctx, s.id); err == nil {
			if row = commandRow(screenRows(screen), text); row >= 0 {
				break
			}
		}
		if time.Now().After(deadline) || !e.pause(ctx) {
			break
		}
	}
	if err := e.cfg.Typist.Keys(ctx, s.id, "Enter"); err != nil {
		return err
	}
	if row < 0 {
		return nil // never seen on screen: nothing to check it against
	}
	for try := 0; ; try++ {
		other, taken := e.letGo(ctx, s, text, row)
		if taken {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if other != "" {
			e.emptyRow(ctx, s, other)
			return &harness.FallbackError{Reason: "source",
				Detail: s.ad.Name + " changed the command in its input line to “" + clipText(other) + "”. Nothing was run."}
		}
		if try >= 1 {
			return &harness.FallbackError{Reason: "source",
				Detail: s.ad.Name + " did not take Enter: “" + clipText(text) + "” is still in its input line. Check the terminal."}
		}
		if err := e.cfg.Typist.Keys(ctx, s.id, "Enter"); err != nil {
			return err
		}
	}
}

// letGo waits for the rows from row down to stop holding text. other is
// the command a completion put there instead, when the lowest row that
// shows a command shows another one.
func (e *Engine) letGo(ctx context.Context, s *sess, text string, row int) (other string, taken bool) {
	deadline := time.Now().Add(e.scaled(submitWait))
	for {
		if screen, err := e.cfg.Screen(ctx, s.id); err == nil {
			rows := screenRows(screen)
			holding := false
			other = ""
			for i := len(rows) - 1; i >= min(row, len(rows)); i-- {
				if endsWith(rows[i], text) {
					holding = !newPromptBelow(rows, i, text)
					break
				}
				if other == "" && strings.TrimSpace(rows[i]) != "" {
					if cmd := rowCommand(rows[i]); cmd != "" {
						other = cmd
					}
				}
			}
			if !holding && other == "" {
				return "", true
			}
			if !holding && time.Now().After(deadline) {
				return other, false
			}
			if holding {
				other = ""
			}
		}
		if time.Now().After(deadline) || !e.pause(ctx) {
			return other, false
		}
	}
}

// newPromptBelow reports whether a row under rows[i], which holds text
// after its prompt mark, starts with the same mark: the TUI drew a fresh
// input line under the command it took, which it keeps on screen as the
// echo of what was sent (Goose 1.57: "> /mode chat", its reply, then
// "> Enter to send · Ctrl+J newline").
func newPromptBelow(rows []string, i int, text string) bool {
	r := strings.TrimSpace(strings.Trim(rows[i], "│┃| "))
	k := strings.LastIndex(r, strings.TrimSpace(text))
	if k <= 0 {
		return false
	}
	mark := strings.TrimSpace(r[:k])
	if mark == "" || strings.ContainsAny(mark, " /") {
		return false
	}
	for _, below := range rows[i+1:] {
		b := strings.TrimSpace(strings.Trim(below, "│┃| "))
		if b == mark || strings.HasPrefix(b, mark+" ") {
			return true
		}
	}
	return false
}

// emptyRow backspaces a command a completion left in the input line.
func (e *Engine) emptyRow(ctx context.Context, s *sess, cmd string) {
	keys := make([]string, min(utf8.RuneCountInString(cmd)+8, maxBackspaces))
	for i := range keys {
		keys[i] = "BSpace"
	}
	_ = e.cfg.Typist.Keys(ctx, s.id, keys...)
}
