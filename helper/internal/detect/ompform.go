package detect

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// omp's ask tool takes one HTML form from 18.20 (all of its asks on 19)
// and draws it as one stacked dialog, seen on 19.0.1
// (testdata/omp/real-form-*.txt):
//
//	╭─ Ask · 1 of 2 answered ─────────────╮
//	│ ✓ Which color should the button be? │   a folded field: its question
//	│    Red   ✓ Green   Blue              │   and its answer as pills
//	│                                      │
//	│ ❯ Button text *                      │   the focused text field
//	│   │ /Ship it                         │   its value row
//	│                                      │
//	│   Submit                             │
//	╰──────────────────────────────────────╯
//
// The field holding focus is open: a choice field's option rows
// ("❯ ○ Red", "  ☑ iOS", "  ○ Other: type your own") under its question,
// a text field's label with the cursor and its value rows under a bar, a
// dropdown's label and "‹ Europe ›  1/2". There is no key-hint footer.
// The marks follow omp's symbol preset (unicode, nerd font or ascii).

// FormStop is the kind of row that holds the form's focus.
type FormStop int

const (
	FormNone   FormStop = iota
	FormOption          // an option row of a radio or check-box group
	FormOther           // the group's "Other:" row, a text field of its own
	FormSelect          // a dropdown
	FormText            // a text field or text area
	FormSubmit          // the Submit row
)

// Form is the focused part of omp's form dialog.
type Form struct {
	Stop FormStop
	// Prompt is the question of the focused field: the label of a text
	// field or dropdown, the line above a group's options.
	Prompt string
	// Options are the focused group's rows, Other last (Text set);
	// Cursor is the index of the focused one.
	Options []ChoiceOption
	Cursor  int
	Multi   bool
	// Value is the dropdown's value, the text field's text (its rows
	// joined by a space), or the Other row's typed text.
	Value string
	// Lines is the dialog's body, unboxed.
	Lines []string
}

var (
	formTitle = regexp.MustCompile(`^\s*[╭┌]─+\s*Ask(?: · \d+ of \d+ answered)?(?: \(\d+s\))?\s*─+[╮┐]\s*$`)
	formEnd   = regexp.MustCompile(`^\s*[╰└]─+[╯┘]\s*$`)
	// formOption: cursor or two spaces, a mark of any of omp's symbol
	// presets, the label.
	formOption = regexp.MustCompile(`^(?:([❯>\x{f054}]) |  )(○|◉|●|☐|☑|\(o\)|\( \)|\[x\]|\[ \]|[\x{f10c}\x{f192}\x{f096}\x{f14a}]) (.*)$`)
	formCursor = regexp.MustCompile(`^[❯>\x{f054}] (.*)$`)
	formSelect = regexp.MustCompile(`^‹ (.*?) ›(?:\s+\d+/\d+)?$`)
	// formBounds is a text field's dim suffix: its required star and
	// "  ≥ 3", "  1–5".
	formBounds = regexp.MustCompile(`(?: \*)?(?:\s{2,}(?:[≥≤] ?\S+|\S+–\S+))?$`)
)

const otherPrefix = "Other: "

// formPlaceholder is what an empty Other row shows.
const formPlaceholder = "type your own"

func checkedMark(m string) bool {
	switch m {
	case "◉", "●", "☑", "(o)", "[x]", "", "":
		return true
	}
	return false
}

func boxMark(m string) bool {
	switch m {
	case "☐", "☑", "[ ]", "[x]", "", "":
		return true
	}
	return false
}

// formBody returns the lines inside the last omp form dialog on screen,
// unboxed, blank lines kept, or false.
func formBody(screen string) ([]string, bool) {
	if len(screen) > maxScreenBytes {
		screen = screen[len(screen)-maxScreenBytes:]
	}
	all := strings.Split(ansiSeq.ReplaceAllString(screen, ""), "\n")
	top, end := -1, -1
	for i := len(all) - 1; i >= 0; i-- {
		l := strings.TrimRight(stripControls(all[i]), " ")
		if end < 0 && formEnd.MatchString(l) {
			end = i
			continue
		}
		if end >= 0 && formTitle.MatchString(l) {
			top = i
			break
		}
	}
	if top < 0 || end-top < 2 {
		return nil, false
	}
	var body []string
	for _, l := range all[top+1 : end] {
		l = strings.TrimRight(stripControls(l), " ")
		u := unbox(l)
		if u == l {
			return nil, false // a line out of the box: not one dialog
		}
		body = append(body, strings.TrimPrefix(u, " "))
	}
	return dropScrollbar(body), true
}

// dropScrollbar removes the track or thumb a long form draws in its last
// column, when one is there.
func dropScrollbar(body []string) []string {
	n := 0
	for _, l := range body {
		if tail := []rune(l); len(tail) > 8 && strings.ContainsRune("│█", tail[len(tail)-1]) && tail[len(tail)-2] == ' ' {
			n++
		}
	}
	if n < 3 {
		return body
	}
	out := make([]string, len(body))
	for i, l := range body {
		if tail := []rune(l); len(tail) > 8 && strings.ContainsRune("│█", tail[len(tail)-1]) {
			l = strings.TrimRight(string(tail[:len(tail)-1]), " ")
		}
		out[i] = l
	}
	return out
}

// ParseForm reads omp's form dialog at the bottom of screen.
func ParseForm(screen string) (Form, bool) {
	body, ok := formBody(screen)
	if !ok {
		return Form{}, false
	}
	f := Form{Lines: body, Cursor: -1}
	focus, submit := -1, false
	for i, l := range body {
		if t := strings.TrimSpace(l); t == "Submit" || formCursor.FindStringSubmatch(l) != nil && formCursor.FindStringSubmatch(l)[1] == "Submit" {
			submit = true
		}
		if formCursor.MatchString(l) && focus < 0 {
			focus = i
		}
	}
	if !submit || focus < 0 {
		return Form{}, false
	}
	l := body[focus]
	if m := formOption.FindStringSubmatch(l); m != nil {
		return parseFormGroup(f, body, focus)
	}
	text := formCursor.FindStringSubmatch(l)[1]
	if text == "Submit" {
		f.Stop = FormSubmit
		return f, true
	}
	// A text field or dropdown: the label (wrapped lines indented two),
	// then its value rows under a bar.
	label := []string{text}
	i := focus + 1
	for ; i < len(body) && strings.HasPrefix(body[i], "  ") && !strings.HasPrefix(strings.TrimLeft(body[i], " "), "│"); i++ {
		label = append(label, strings.TrimSpace(body[i]))
	}
	f.Prompt = strings.TrimSpace(formBounds.ReplaceAllString(normalise(strings.Join(label, " ")), ""))
	var rows []string
	for ; i < len(body); i++ {
		t := strings.TrimLeft(body[i], " ")
		if !strings.HasPrefix(t, "│") {
			break
		}
		rows = append(rows, strings.TrimSpace(strings.TrimPrefix(t, "│")))
	}
	if len(rows) == 0 {
		return Form{}, false
	}
	if m := formSelect.FindStringSubmatch(rows[0]); m != nil && len(rows) == 1 {
		f.Stop, f.Value = FormSelect, strings.TrimSpace(m[1])
		return f, true
	}
	f.Stop = FormText
	var parts []string
	for _, r := range rows {
		if r != "" {
			parts = append(parts, r)
		}
	}
	f.Value = strings.Join(parts, " ")
	return f, true
}

// parseFormGroup reads the option rows around the focused one and the
// question above them.
func parseFormGroup(f Form, body []string, focus int) (Form, bool) {
	start := focus
	for start > 0 && (formOption.MatchString(body[start-1]) || formWrapped(body[start-1])) {
		start--
	}
	for start < focus && !formOption.MatchString(body[start]) {
		start++ // a group starts at a row, not inside a wrapped label
	}
	for i := start; i < len(body); i++ {
		l := body[i]
		m := formOption.FindStringSubmatch(l)
		if m == nil {
			if formWrapped(l) && len(f.Options) > 0 {
				o := &f.Options[len(f.Options)-1]
				o.Label = normalise(o.Label + " " + strings.TrimSpace(l))
				continue
			}
			break
		}
		if len(f.Options) > 0 && boxMark(m[2]) != f.Multi {
			break // another group's rows
		}
		f.Multi = boxMark(m[2])
		o := ChoiceOption{Label: normalise(m[3]), Checked: checkedMark(m[2])}
		if rest, ok := strings.CutPrefix(m[3], otherPrefix); ok || m[3] == strings.TrimSpace(otherPrefix) {
			o.Text, o.Label = true, "Other"
			if v := strings.TrimSpace(rest); v != formPlaceholder {
				o.Detail = v
			}
		}
		if i == focus {
			f.Cursor = len(f.Options)
		}
		f.Options = append(f.Options, o)
	}
	if f.Cursor < 0 {
		return Form{}, false
	}
	// The question: the block of words above the rows, up to a blank line.
	var head []string
	for i := start - 1; i >= 0 && strings.TrimSpace(body[i]) != ""; i-- {
		head = append([]string{strings.TrimSpace(body[i])}, head...)
	}
	f.Prompt = normalise(strings.Join(head, " "))
	f.Stop = FormOption
	if c := f.Options[f.Cursor]; c.Text {
		f.Stop, f.Value = FormOther, c.Detail
	}
	return f, f.Prompt != ""
}

// formWrapped is a wrapped label's next line: indented past the marks.
func formWrapped(l string) bool {
	return strings.HasPrefix(l, "    ") && strings.TrimSpace(l) != "" && utf8.RuneCountInString(l) > 4
}
