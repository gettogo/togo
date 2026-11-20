package omp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// ask-form-19.jsonl is a real omp 19.0.1 session (scrubbed: the folder,
// and the model-usage, reminder and device-notice records dropped) with
// two form asks answered at the terminal: a radio group and a required
// text field, then a check-box group, a dropdown and a text area. Both are
// offered to the phone, with what each field requires and starts with,
// and close with the answers omp recorded.
func TestRealFormAsksAreAnswerable(t *testing.T) {
	s := newSession(env())
	open := map[string]harness.Item{}
	closed := map[string]harness.Item{}
	for _, r := range records(t, "ask-form-19.jsonl", "session") {
		items, _ := s.Map(r)
		for _, it := range items {
			if it.Kind != harness.KindQuestion {
				continue
			}
			if len(it.Question.Answered) == 0 {
				open[it.ID] = it
			} else {
				closed[it.ID] = it
			}
		}
	}
	if len(open) != 2 || len(closed) != 2 {
		t.Fatalf("open %d, closed %d questions; want 2 and 2", len(open), len(closed))
	}
	type field struct {
		id, prompt string
		options    string
		multi      bool
		text       bool
		required   bool
		def        []string
	}
	want := map[string][]field{
		"ask-call_a06cee96344a4b198e2992fe|fc_tmp_0sxfhi0q67c": {
			{"color", "Which color should the button be?", "Red,Green,Blue", false, true, false, nil},
			{"label", "Button text", "", false, true, true, nil},
		},
		"ask-call_35a6df425c5d499986b81eb4|fc_tmp_64arw3ngket": {
			{"targets", "Which platforms?", "iOS,macOS,Linux", true, true, false, []string{"iOS"}},
			{"region", "Region", "Europe,United States", false, false, false, []string{"Europe"}},
			{"notes", "Notes", "", false, true, false, nil},
		},
	}
	answered := map[string]map[string][]string{
		"ask-call_a06cee96344a4b198e2992fe|fc_tmp_0sxfhi0q67c": {"color": {"Green"}, "label": {"/Ship it — ñ 100%! (a|b)*"}},
		"ask-call_35a6df425c5d499986b81eb4|fc_tmp_64arw3ngket": {"targets": {"iOS, macOS"}, "region": {"United States"}, "notes": {"two words"}},
	}
	for id, fields := range want {
		it, ok := open[id]
		if !ok {
			t.Fatalf("no question %s (have %v)", id, open)
		}
		if it.Question.Answerable != nil {
			t.Errorf("%s: not answerable from the phone", id)
		}
		if len(it.Question.Questions) != len(fields) {
			t.Fatalf("%s: %d questions", id, len(it.Question.Questions))
		}
		for i, f := range fields {
			q := it.Question.Questions[i]
			var opts []string
			for _, o := range q.Options {
				opts = append(opts, o.ID)
			}
			got := field{q.ID, q.Prompt, strings.Join(opts, ","), q.Multi, q.AllowText, q.Required != nil && *q.Required, q.Default}
			if !reflect.DeepEqual(got, f) {
				t.Errorf("%s question %d:\n got %+v\nwant %+v", id, i, got, f)
			}
		}
		if got := closed[id].Question.Answered; !reflect.DeepEqual(got, answered[id]) {
			t.Errorf("%s answered %v, want %v", id, got, answered[id])
		}
	}
}

// Forms whose fields the key driver cannot find on screen, or fill and see
// filled, stay answerable at the terminal only.
func TestFormsThePhoneCannotFill(t *testing.T) {
	radios := `<fieldset><legend>Pick</legend><label><input type="radio" name="p" value="a">A</label><label><input type="radio" name="p" value="b">B</label></fieldset>`
	for _, c := range []struct{ name, html string }{
		{"password", `<form>` + radios + `<label>Token <input type="password" name="t"></label></form>`},
		{"date", `<form><label>When <input type="date" name="d"></label></form>`},
		{"disabled option", `<form><fieldset><legend>Pick</legend><label><input type="radio" name="p" value="a">A</label><label><input type="radio" name="p" value="b" disabled>B</label></fieldset></form>`},
		{"two fields in one fieldset", `<form><fieldset><legend>Deploy</legend><label>Name <input name="n"></label><label>Tag <input name="t"></label></fieldset></form>`},
		{"lone check box", `<form><fieldset><legend>Terms</legend><label><input type="checkbox" name="ok">I agree</label></fieldset></form>`},
		{"same question twice", `<form><label>Name <input name="a"></label><label>Name <input name="b"></label></form>`},
		{"no label", `<form><input name="x" placeholder="Something"></form>`},
		{"radios with no legend", `<form><label><input type="radio" name="p" value="a">A</label><label><input type="radio" name="p" value="b">B</label></form>`},
		{"multiple select", `<form><label>Tags <select name="t" multiple><option>a</option><option>b</option></select></label></form>`},
	} {
		qs, drivable := formQuestions(c.html)
		if len(qs) == 0 || drivable {
			t.Errorf("%s: %d questions, drivable %v", c.name, len(qs), drivable)
		}
		it := askQuestion("c", "t", "", t0, map[string]any{"html": c.html})
		if it == nil || it.Question.Answerable == nil || *it.Question.Answerable {
			t.Errorf("%s: offered to the phone: %+v", c.name, it)
		}
	}
	// An option labelled Other is the group's own: omp adds no Other row,
	// so the question takes no typed words.
	qs, drivable := formQuestions(`<form><fieldset><legend>Pick</legend><label><input type="radio" name="p" value="a">A</label><label><input type="radio" name="p" value="o">Other (say which)</label></fieldset></form>`)
	if !drivable || qs[0].AllowText {
		t.Errorf("own Other row: %+v drivable %v", qs, drivable)
	}
}
