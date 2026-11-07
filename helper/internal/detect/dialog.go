package detect

import (
	"regexp"
	"strings"
)

// A modal dialog is anything on screen that takes the keys typed next
// instead of the agent's input: an "Update available" box (OpenCode 1.x
// confirmed its update when a prompt was typed into it), a folder trust
// question, a press-a-key splash. Detect finds the ones that are answer
// menus; Dialog adds boxes whose buttons are words ("Confirm", "Cancel")
// and splash screens, which Detect leaves alone because they are not
// questions the phone can answer.
//
// Dialog backs the typing guard only: nothing is typed while it reports
// one. A false positive holds a prompt back with a notice, which is cheap;
// a miss can confirm an update. Each shape still needs two cues, a title
// phrase and, within a few lines, a button row or key hint, so a chat that
// merely mentions "update available" does not match.

// DialogInfo is a dialog found on screen.
type DialogInfo struct {
	// Title is the line that names the dialog, cleaned for display.
	Title string
	// Prompt is set when the dialog is also an answerable prompt (Detect
	// recognised it): the approvals path can route it to the phone.
	Prompt *Prompt
}

// dialogTitle matches the line naming a modal dialog.
var dialogTitle = regexp.MustCompile(`(?i)(\bupdate (is )?available\b|\bnew (version|release|update)\b.*\bavailable\b|` +
	`\bwould you like to (update|upgrade|install|restart)\b|\b(update|upgrade|restart) now\?|\brestart to (update|apply)\b|` +
	`\bdo you trust\b|\btrust (this|the) (folder|files|directory|workspace|project)\b|` +
	`\bpress (enter|any key|return) to (continue|start|begin)\b|\bwhat'?s new in\b|\bfirst[- ]run setup\b)`)

// dialogButton is one button word.
var dialogButton = regexp.MustCompile(`(?i)^(yes|no|ok|okay|confirm|cancel|update|update now|upgrade|install|skip|later|not now|` +
	`dismiss|continue|exit|quit|close|trust|don'?t trust|restart|remind me later)$`)

// dialogHint is a key hint inside a dialog.
var dialogHint = regexp.MustCompile(`(?i)(\besc(ape)?\b.*\b(cancel|close|dismiss|skip|back)\b|\benter\b.*\b(confirm|select|continue|accept|update)\b|` +
	`[↵⏎]|\[y/n\]|\(y/n\)|\by/n\b|\btab\b.*\b(switch|next)\b|\bpress (enter|any key|return)\b)`)

// dialogSpan is how many lines below the title the buttons may sit.
const dialogSpan = 8

// Dialog reports a modal dialog on screen, if any.
func Dialog(screen string) (DialogInfo, bool) {
	if p, ok := Detect(screen); ok {
		title := p.Title
		return DialogInfo{Title: title, Prompt: &p}, true
	}
	raw := window(screen)
	lines := make([]string, len(raw))
	for i, l := range raw {
		lines[i] = strings.TrimSpace(unbox(l))
	}
	for i, l := range lines {
		if !dialogTitle.MatchString(l) {
			continue
		}
		end := min(len(lines), i+dialogSpan+1)
		for j := i; j < end; j++ {
			if dialogHint.MatchString(lines[j]) || buttonRow(lines[j]) {
				return DialogInfo{Title: truncate(cleanTitle(l), 120)}, true
			}
		}
	}
	return DialogInfo{}, false
}

// buttonSplit cuts a button row into labels: brackets, angle brackets,
// pipes and runs of two or more spaces separate buttons.
var buttonSplit = regexp.MustCompile(`[\[\]<>|()]+|\s{2,}`)

// buttonRow reports a row of button words: at least one, and nothing on
// the row that is not a button (a sentence that says "update" is not a
// row of buttons).
func buttonRow(line string) bool {
	if line == "" || len(line) > 80 {
		return false
	}
	n := 0
	for _, f := range buttonSplit.Split(line, -1) {
		f = strings.Trim(strings.TrimSpace(f), "❯›>*•·:")
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !dialogButton.MatchString(f) {
			return false
		}
		n++
	}
	return n > 0
}

// cleanTitle drops border runs and symbols around a title line.
func cleanTitle(l string) string {
	l = strings.TrimSpace(strings.Trim(l, "─━═│┃║╭╮╰╯┌┐└┘▐▌█▀▄ *·•"))
	return strings.TrimSpace(trailingEsc.ReplaceAllString(l, ""))
}

// trailingEsc is a dialog's close hint at the end of its title row.
var trailingEsc = regexp.MustCompile(`(?i)\s{2,}(esc|\[esc\]|×|x)$`)
