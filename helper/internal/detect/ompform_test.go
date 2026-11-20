package detect

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// The form dialog omp 19.0.1 drew for two real asks, one capture per step
// (testdata/omp/real-form-*.txt).
func TestParseFormReadsTheFocusedField(t *testing.T) {
	opts := func(labels string, checked ...string) []ChoiceOption {
		var out []ChoiceOption
		for _, l := range strings.Split(labels, ",") {
			out = append(out, ChoiceOption{Label: l, Checked: contains(checked, l)})
		}
		return append(out, ChoiceOption{Label: "Other", Text: true})
	}
	colors := "Red,Green,Blue"
	for _, c := range []struct {
		file string
		want Form
	}{
		{"open", Form{Stop: FormOption, Prompt: "Which color should the button be?", Options: opts(colors), Cursor: 0}},
		{"moved", Form{Stop: FormOption, Prompt: "Which color should the button be?", Options: opts(colors), Cursor: 1}},
		{"text-empty", Form{Stop: FormText, Prompt: "Button text", Cursor: -1, Rows: 1}},
		{"text-typed", Form{Stop: FormText, Prompt: "Button text", Value: "/Ship it — ñ 100%! (a|b)*", Cursor: -1, Rows: 1}},
		{"submit", Form{Stop: FormSubmit, Cursor: -1}},
		{"checkbox", Form{Stop: FormOption, Prompt: "Which platforms?", Options: opts("iOS,macOS,Linux", "iOS"), Multi: true, Cursor: 0}},
		{"checkbox-toggled", Form{Stop: FormOption, Prompt: "Which platforms?", Options: opts("iOS,macOS,Linux", "iOS", "macOS"), Multi: true, Cursor: 1}},
		{"select", Form{Stop: FormSelect, Prompt: "Region", Value: "Europe", Cursor: -1}},
		{"select-changed", Form{Stop: FormSelect, Prompt: "Region", Value: "United States", Cursor: -1}},
		{"textarea-typed", Form{Stop: FormText, Prompt: "Notes", Value: "two words", Cursor: -1, Rows: 4}},
		// Other typed at the desk, then Red picked: Other no longer counts.
		{"desk-other-kept", Form{Stop: FormOption, Prompt: "Which color?", Cursor: 0, Options: []ChoiceOption{
			{Label: "Red", Checked: true}, {Label: "Green"}, {Label: "Other", Detail: "desk words", Text: true}}}},
		// A limit after the star: "Count *  ≥ 3".
		{"limit-suffix", Form{Stop: FormText, Prompt: "Count", Cursor: -1, Rows: 1}},
		// Longer than the field: the input shows the end, with no ellipsis.
		{"long-text", Form{Stop: FormText, Prompt: "Long text", Cursor: -1, Rows: 1,
			Value: "ver the lazy dog and keeps running past the river bank into the old forest beyond the hills"}},
		{"long-folded", Form{Stop: FormSubmit, Cursor: -1}},
		// Submit sent the person back to a field that omp refuses.
		{"validation", Form{Stop: FormText, Prompt: "Count", Value: "2", Cursor: -1, Rows: 1, Error: "Must be at least 3", Fix: 1}},
		// Typed into the Other row by the phone's answer (live, through the helper).
		{"other-typed", Form{Stop: FormOther, Prompt: "Which color should the button be?", Value: "Teal (not listed)", Cursor: 3,
			Options: append(opts(colors)[:3], ChoiceOption{Label: "Other", Detail: "Teal (not listed)", Checked: true, Text: true})}},
	} {
		raw, err := os.ReadFile("testdata/omp/real-form-" + c.file + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		got, ok := ParseForm(string(raw))
		if !ok {
			t.Errorf("%s: no form", c.file)
			continue
		}
		if got.Width != 96 {
			t.Errorf("%s: width %d, want 96", c.file, got.Width)
		}
		got.Lines, got.Width, got.Values = nil, 0, nil
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.file, got, c.want)
		}
	}
	// Answered: the dialog left the screen and the answers show in the
	// transcript.
	raw, _ := os.ReadFile("testdata/omp/real-form-gone.txt")
	if f, ok := ParseForm(string(raw)); ok {
		t.Errorf("answered screen read as a form: %+v", f)
	}
	// Esc at the desk gave the keys to omp's composer: the dialog draws no
	// cursor, and a key sent now would be typed into the composer.
	raw, _ = os.ReadFile("testdata/omp/real-form-released.txt")
	if f, ok := ParseForm(string(raw)); ok {
		t.Errorf("a released dialog read as one taking keys: %+v", f)
	}
	// The colours change nothing.
	raw, _ = os.ReadFile("testdata/omp/real-form-open.ansi")
	if f, ok := ParseForm(string(raw)); !ok || f.Stop != FormOption || f.Cursor != 0 || f.Prompt != "Which color should the button be?" {
		t.Errorf("coloured capture: %+v, %v", f, ok)
	}
}

func contains(list []string, s string) bool {
	for _, l := range list {
		if l == s {
			return true
		}
	}
	return false
}

// omp's other symbol presets, a wrapped option label, a typed Other answer
// and a long form's scrollbar.
func TestParseFormPresetsAndShapes(t *testing.T) {
	box := func(rows ...string) string {
		var b strings.Builder
		b.WriteString("╭─ Ask · 0 of 2 answered " + strings.Repeat("─", 30) + "╮\n")
		for _, r := range rows {
			pad := 54 - len([]rune(r))
			if pad < 0 {
				pad = 0
			}
			b.WriteString("│ " + r + strings.Repeat(" ", pad) + "│\n")
		}
		b.WriteString("╰" + strings.Repeat("─", 56) + "╯\n")
		return b.String()
	}
	ascii := box("Pick one", "  (o) Alpha", "> ( ) Beta", "  ( ) Other: type your own", "", "  Submit")
	f, ok := ParseForm(ascii)
	if !ok || f.Stop != FormOption || f.Cursor != 1 || !f.Options[0].Checked || f.Multi || f.Prompt != "Pick one" {
		t.Errorf("ascii preset: %+v %v", f, ok)
	}
	nerd := box("Pick any", "\uf054 \uf14a Alpha", "  \uf096 Beta", "", "  Submit")
	if f, ok := ParseForm(nerd); !ok || !f.Multi || !f.Options[0].Checked || f.Options[1].Checked {
		t.Errorf("nerd preset: %+v %v", f, ok)
	}
	if f, ok := ParseForm(box("❯ Name *  ≥ 3", "  │ ab", "    Must be at least 3 characters", "", "  Submit  1 to fix")); !ok || f.Prompt != "Name" || f.Error != "Must be at least 3 characters" || f.Fix != 1 {
		t.Errorf("limit and error: %+v %v", f, ok)
	}
	if f, ok := ParseForm(box("Pick", "  ○ A", "❯ ○ Other:  type your own", "", "  Submit")); !ok || f.Stop != FormOther || f.Value != "" {
		t.Errorf("focused empty Other row: %+v %v", f, ok)
	}
	wrapped := box("Which plan?", "  ○ Postgres: relational, JSONB support and a", "    long description", "❯ ○ Other: Cockroach; 50% (or |more|)", "", "  Submit")
	f, ok = ParseForm(wrapped)
	if !ok || f.Stop != FormOther || f.Value != "Cockroach; 50% (or |more|)" || f.Options[0].Label != "Postgres: relational, JSONB support and a long description" {
		t.Errorf("wrapped and typed: %+v %v", f, ok)
	}
	var rows []string
	for i := 0; i < 6; i++ {
		rows = append(rows, "Line "+strings.Repeat("x", i)+strings.Repeat(" ", 40)+"█")
	}
	rows = append(rows, "❯ Name *"+strings.Repeat(" ", 40)+"│", "  │ ada"+strings.Repeat(" ", 40)+"│", "", "  Submit"+strings.Repeat(" ", 40)+"│")
	if f, ok := ParseForm(box(rows...)); !ok || f.Stop != FormText || f.Prompt != "Name" || f.Value != "ada" || !f.Scrolled {
		t.Errorf("scrollbar: %+v %v", f, ok)
	}
	// Not the form: omp 18.16's ask dialog, and a transcript that quotes one.
	raw, _ := os.ReadFile("testdata/omp/real-ask-single.txt")
	if f, ok := ParseForm(string(raw)); ok {
		t.Errorf("18.16 dialog read as a form: %+v", f)
	}
	if _, ok := ParseForm("╭─ Ask ─────╮\n  ❯ ○ Red\n│   Submit │\n╰──────────╯\n"); ok {
		t.Error("a broken box read as a form")
	}
}
