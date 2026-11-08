package detect

import (
	"regexp"
	"strings"
	"unicode"
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
	// Dismiss, when set, are keys that close the dialog without accepting
	// anything: a splash whose Enter opens a link but whose other keys
	// close it. Never Enter.
	Dismiss []string
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
	if d, ok := linkSplash(screen); ok {
		return d, true
	}
	raw := window(screen)
	lines := make([]string, len(raw))
	for i, l := range raw {
		lines[i] = strings.TrimSpace(unbox(l))
	}
	for i, l := range lines {
		if !dialogTitle.MatchString(l) || bannerAt(lines, i) {
			continue
		}
		end := min(len(lines), i+dialogSpan+1)
		for j := i; j < end; j++ {
			if dialogHint.MatchString(lines[j]) || buttonRow(lines[j]) && !wrappedTail(lines, j) {
				return DialogInfo{Title: truncate(cleanTitle(l), 120)}, true
			}
		}
	}
	return DialogInfo{}, false
}

// runUpdate is the instruction a non-modal update notice gives instead of
// buttons: "Run: omp update", "run `npm i -g x` to update".
var runUpdate = regexp.MustCompile(`(?i)\brun:?\s+\S.*\b(update|upgrade|install)\b`)

// bannerAt reports an update notice that tells the user what to run: it
// takes no keys, so key hints below it belong to something else (omp's
// slash-command popup) and it does not hold typing back.
func bannerAt(lines []string, i int) bool {
	for j := i; j < min(len(lines), i+3); j++ {
		if runUpdate.MatchString(lines[j]) {
			return true
		}
	}
	return false
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

// wrappedTail reports a one-word "button row" that is the end of the line
// above wrapped at the pane's width. Seen on omp 18.16 at a phone's 47
// columns: its update banner's "Run: omp update" left "update" alone on a
// line, and every typed command was refused as if a dialog were up.
func wrappedTail(lines []string, j int) bool {
	if j == 0 || len(buttonSplit.Split(strings.TrimSpace(lines[j]), -1)) != 1 {
		return false
	}
	prev := lines[j-1]
	if prev == "" || buttonRow(prev) {
		return false
	}
	r := []rune(prev)
	last := r[len(r)-1]
	return unicode.IsLetter(last) || unicode.IsDigit(last)
}

// cleanTitle drops border runs and symbols around a title line.
func cleanTitle(l string) string {
	l = strings.TrimSpace(strings.Trim(l, "─━═│┃║╭╮╰╯┌┐└┘▐▌█▀▄ *·•"))
	return strings.TrimSpace(trailingEsc.ReplaceAllString(l, ""))
}

// trailingEsc is a dialog's close hint at the end of its title row.
var trailingEsc = regexp.MustCompile(`(?i)\s{2,}(esc|\[esc\]|×|x)$`)

// enterOpens is a key hint whose Enter opens something outside the
// terminal: Cline 3.0.74's "Introducing Cline Desktop" splash reads
// "Press Enter to open, any other key to close" under a link, and Enter
// ran the system "open" on it. Read on two joined lines: a phone's 47
// columns wrap the hint.
var enterOpens = regexp.MustCompile(`(?i)\b(enter|return|↵|⏎)\b[^.;]{0,24}\bto (open|launch|visit)\b|` +
	`\b(enter|return)\b[^.;]{0,40}\b(browser|link|url|website)\b`)

// otherKeyCloses is the other half of such a hint: which key closes it.
var otherKeyCloses = regexp.MustCompile(`(?i)\b(any other key|esc(ape)?|q)\b[^.;]{0,16}\b(to )?(close|dismiss|skip|exit)\b`)

// splashTitle is the heading of a feature announcement splash.
var splashTitle = regexp.MustCompile(`(?i)^(introducing|announcing|new:|what'?s new|meet)\b`)

// EnterOpensLink reports a screen whose Enter key opens a link or app:
// nothing may send Enter to it.
func EnterOpensLink(screen string) bool {
	lines := cleanLines(screen)
	for i := range lines {
		if enterOpens.MatchString(joinNext(lines, i)) {
			return true
		}
	}
	return false
}

// linkSplash finds a splash whose Enter opens a link and whose Escape
// closes it, and offers Escape to dismiss it.
func linkSplash(screen string) (DialogInfo, bool) {
	lines := cleanLines(screen)
	for i := len(lines) - 1; i >= 0; i-- {
		hint := joinNext(lines, i)
		if !enterOpens.MatchString(hint) {
			continue
		}
		d := DialogInfo{Title: "a splash screen"}
		for j := i - 1; j >= 0; j-- {
			if splashTitle.MatchString(lines[j]) {
				d.Title = truncate(cleanTitle(lines[j]), 120)
				break
			}
		}
		if otherKeyCloses.MatchString(hint) {
			d.Dismiss = []string{"Escape"}
		}
		return d, true
	}
	return DialogInfo{}, false
}

func cleanLines(screen string) []string {
	raw := window(screen)
	lines := make([]string, len(raw))
	for i, l := range raw {
		lines[i] = strings.TrimSpace(unbox(l))
	}
	return lines
}

func joinNext(lines []string, i int) string {
	if i+1 < len(lines) {
		return lines[i] + " " + lines[i+1]
	}
	return lines[i]
}
