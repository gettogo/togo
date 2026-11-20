package approval

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/tmux"
)

// formSim draws omp 19.0.1's form dialog the way the captures in
// detect/testdata/omp/real-form-*.txt show it, and takes keys like its
// HtmlFormDialog (packages/tui/src/html-form/dialog.ts): Up and Down move
// between rows, Tab and Shift+Tab between fields, Space picks a radio or
// flips a box, Enter picks a radio and moves on, Left and Right step a
// dropdown, typing goes into the focused input, End and C-u empty it,
// Enter on Submit checks the required fields and closes the dialog.
// Escape, C-c and Tab on Submit hand the keys to the composer: released.
type formSim struct {
	mu       sync.Mutex
	fields   []*simField
	focus    int // index into stops()
	closed   bool
	released bool
	deaf     bool // typed words are lost
	sent     [][]string
}

type simField struct {
	prompt   string
	kind     string // radio, checkbox, select, text, textarea
	opts     []string
	checked  map[int]bool
	other    string
	value    string
	required bool
}

type simStop struct {
	f      int
	option int // -1 for the field's own stop, len(opts) for Other
}

func (s *formSim) stops() []simStop {
	var out []simStop
	for i, f := range s.fields {
		switch f.kind {
		case "radio", "checkbox":
			for o := range f.opts {
				out = append(out, simStop{i, o})
			}
			out = append(out, simStop{i, len(f.opts)})
		default:
			out = append(out, simStop{i, -1})
		}
	}
	return append(out, simStop{-1, -1})
}

func (s *formSim) ListSessions(context.Context) ([]tmux.SessionInfo, error) { return nil, nil }
func (s *formSim) PasteText(context.Context, string, string) error          { return nil }

func (s *formSim) CapturePane(context.Context, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	b.WriteString(" Thinking about the form.\n")
	if s.closed {
		b.WriteString("╭─── ? Ask ───╮\n│  answered   │\n╰─────────────╯\n π > ◒ Model\n╰─\n")
		return b.String(), nil
	}
	b.WriteString("╭─ Ask · 0 of " + fmt.Sprint(len(s.fields)) + " answered " + strings.Repeat("─", 40) + "╮\n")
	stops := s.stops()
	cur := stops[s.focus]
	for i, f := range s.fields {
		if i > 0 {
			b.WriteString(boxLine(""))
		}
		if cur.f != i {
			b.WriteString(boxLine("○ " + f.prompt))
			b.WriteString(boxLine("   " + s.shown(f)))
			continue
		}
		switch f.kind {
		case "radio", "checkbox":
			b.WriteString(boxLine(f.prompt))
			for o, label := range append(append([]string{}, f.opts...), "Other: ") {
				c := "  "
				if cur.option == o {
					c = "❯ "
				}
				mark := map[bool]string{false: "○", true: "◉"}[f.checked[o]]
				if f.kind == "checkbox" {
					mark = map[bool]string{false: "☐", true: "☑"}[f.checked[o]]
				}
				if o == len(f.opts) {
					label += f.other
					if f.other == "" {
						label += "type your own"
					}
				}
				b.WriteString(boxLine(c + mark + " " + label))
			}
		case "select":
			b.WriteString(boxLine("❯ " + f.prompt))
			b.WriteString(boxLine("  │ ‹ " + f.opts[s.selected(f)] + " ›  " + fmt.Sprintf("%d/%d", s.selected(f)+1, len(f.opts))))
		default:
			star := ""
			if f.required {
				star = " *"
			}
			b.WriteString(boxLine("❯ " + f.prompt + star))
			b.WriteString(boxLine("  │ " + f.value))
			if f.kind == "textarea" {
				b.WriteString(boxLine("  │"))
			}
		}
	}
	b.WriteString(boxLine(""))
	if cur.f < 0 {
		b.WriteString(boxLine("❯ Submit"))
	} else {
		b.WriteString(boxLine("  Submit"))
	}
	b.WriteString("╰" + strings.Repeat("─", simWidth-2) + "╯\n")
	return b.String(), nil
}

func (s *formSim) selected(f *simField) int {
	for o := range f.opts {
		if f.checked[o] {
			return o
		}
	}
	return 0
}

func (s *formSim) shown(f *simField) string {
	switch f.kind {
	case "text", "textarea":
		return f.value
	}
	var on []string
	for o, l := range f.opts {
		if f.checked[o] {
			on = append(on, "✓ "+l)
		}
	}
	return strings.Join(on, "   ")
}

func (s *formSim) SendKeys(_ context.Context, _ string, literal bool, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, append([]string{fmt.Sprint(literal)}, keys...))
	if s.closed || s.released {
		return nil
	}
	stops := s.stops()
	for _, k := range keys {
		cur := stops[s.focus]
		var f *simField
		if cur.f >= 0 {
			f = s.fields[cur.f]
		}
		input := f != nil && (f.kind == "text" || f.kind == "textarea" || cur.option == len(f.opts))
		switch {
		case literal:
			if input && !s.deaf {
				if cur.option == len(f.opts) {
					f.other += k
				} else {
					f.value += k
				}
			}
		case k == "Escape" || k == "C-c" || k == "Tab" && f == nil:
			s.released = true
			return nil
		case k == "Tab" || k == "BTab":
			s.moveField(map[string]int{"Tab": 1, "BTab": -1}[k])
		case k == "Up" && s.focus > 0:
			s.focus--
		case k == "Down" && s.focus < len(stops)-1:
			s.focus++
		case k == "End":
		case k == "C-u" && input:
			if cur.option >= 0 && f.kind != "text" && f.kind != "textarea" {
				f.other = ""
			} else {
				f.value = ""
			}
		case (k == "Left" || k == "Right") && f != nil && f.kind == "select":
			o := s.selected(f) + map[string]int{"Left": -1, "Right": 1}[k]
			if o >= 0 && o < len(f.opts) {
				f.checked = map[int]bool{o: true}
			}
		case k == "Space" && f != nil && cur.option >= 0 && cur.option < len(f.opts):
			s.toggle(f, cur.option)
		case k == "Enter" && f == nil:
			for _, g := range s.fields {
				if g.required && s.shown(g) == "" && g.other == "" {
					return nil // stays open
				}
			}
			s.closed = true
		case k == "Enter":
			if f.kind == "radio" && cur.option == len(f.opts) && f.other != "" {
				f.checked = map[int]bool{}
			} else if f.kind == "radio" && cur.option >= 0 && cur.option < len(f.opts) {
				s.toggle(f, cur.option)
			}
			s.moveField(1)
		}
	}
	return nil
}

func (s *formSim) toggle(f *simField, o int) {
	if f.kind == "radio" {
		f.checked = map[int]bool{o: true}
		return
	}
	f.checked[o] = !f.checked[o]
}

func (s *formSim) moveField(delta int) {
	stops := s.stops()
	i := s.focus
	for i+delta >= 0 && i+delta < len(stops) && stops[i+delta].f == stops[s.focus].f {
		i += delta
	}
	if i+delta >= 0 && i+delta < len(stops) {
		i += delta
	}
	target := stops[i].f
	for j, st := range stops { // a field's first stop
		if st.f == target {
			s.focus = j
			break
		}
	}
}

func colorForm() *formSim {
	return &formSim{fields: []*simField{
		{prompt: "Which color should the button be?", kind: "radio", opts: []string{"Red", "Green", "Blue"}, checked: map[int]bool{}},
		{prompt: "Button text", kind: "text", required: true},
	}}
}

func TestAnswerFormDrivesEveryFieldAndSubmits(t *testing.T) {
	fastAsk(t)
	sim := colorForm()
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	text := "/Ship it — ñ 100%! (a|b)*;"
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{
		{Prompt: "Which color should the button be?", Labels: []string{"Green"}},
		{Prompt: "Button text", Text: text},
	})
	if err != nil {
		t.Fatalf("%v (keys %v)", err, sim.sent)
	}
	if !sim.closed || sim.released || !sim.fields[0].checked[1] || sim.fields[1].value != text {
		t.Fatalf("closed %v released %v fields %+v %+v", sim.closed, sim.released, sim.fields[0], sim.fields[1])
	}
	want := [][]string{{"false", "Down"}, {"false", "Space"}, {"false", "Tab"}, {"true", text}, {"false", "Tab"}, {"false", "Enter"}}
	if !reflect.DeepEqual(sim.sent, want) {
		t.Fatalf("keys %v\nwant %v", sim.sent, want)
	}
}

func TestAnswerFormCheckBoxesDropdownOtherAndSkippedField(t *testing.T) {
	fastAsk(t)
	sim := &formSim{fields: []*simField{
		{prompt: "Which platforms?", kind: "checkbox", opts: []string{"iOS", "macOS", "Linux"}, checked: map[int]bool{0: true}},
		{prompt: "Region", kind: "select", opts: []string{"Europe", "United States"}, checked: map[int]bool{0: true}},
		{prompt: "Notes", kind: "textarea"},
		{prompt: "Which database?", kind: "radio", opts: []string{"Postgres", "SQLite"}, checked: map[int]bool{0: true}},
	}}
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{
		{Prompt: "Which platforms?", Multi: true, Labels: []string{"Linux"}, Text: "watchOS"},
		{Prompt: "Region", Labels: []string{"United States"}},
		// Notes is optional and left as it is.
		{Prompt: "Which database?", Text: "CockroachDB, 3 nodes"},
	})
	if err != nil {
		t.Fatalf("%v (keys %v)", err, sim.sent)
	}
	f := sim.fields
	if !sim.closed || sim.released || !reflect.DeepEqual(f[0].checked, map[int]bool{0: false, 2: true}) || f[0].other != "watchOS" ||
		sim.selected(f[1]) != 1 || f[2].value != "" || len(f[3].checked) != 0 || f[3].other != "CockroachDB, 3 nodes" {
		t.Fatalf("closed %v released %v: %+v %+v %+v %+v", sim.closed, sim.released, f[0], f[1], f[2], f[3])
	}
	for _, k := range sim.sent {
		if slices.Contains(k, "Escape") || slices.Contains(k, "C-c") {
			t.Fatalf("sent %v", sim.sent)
		}
	}
}

func TestAnswerFormRefusesBeforeAnyKey(t *testing.T) {
	fastAsk(t)
	for _, c := range []struct {
		name    string
		answers []AskAnswer
		msg     string
	}{
		{"line break", []AskAnswer{{Prompt: "Button text", Text: "two\nlines"}}, "line break"},
		{"tab", []AskAnswer{{Prompt: "Button text", Text: "a\tb"}}, "tab"},
		{"too long", []AskAnswer{{Prompt: "Button text", Text: strings.Repeat("x", maxFormText+1)}}, "too long"},
		{"another question", []AskAnswer{{Prompt: "Which size?", Labels: []string{"L"}}}, "different question"},
	} {
		sim := colorForm()
		st := New(Config{Tmux: sim, Pub: &fakePub{}})
		err := st.AnswerChoice(context.Background(), sess, c.answers)
		var ae *AskError
		if !asAskError(err, &ae) || len(sim.sent) != 0 {
			t.Errorf("%s: err %v, keys %v", c.name, err, sim.sent)
		}
	}
	sim := colorForm()
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "Which color should the button be?", Labels: []string{"Purple"}}})
	var ae *AskError
	if !asAskError(err, &ae) || !strings.Contains(ae.Msg, "does not offer") || len(sim.sent) != 0 {
		t.Errorf("an option it lacks: err %v, keys %v", err, sim.sent)
	}
}

// Typed words that never show are taken out again, and nothing is
// submitted.
func TestAnswerFormRemovesTypingThatDidNotShow(t *testing.T) {
	fastAsk(t)
	sim := colorForm()
	sim.deaf = true
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{
		{Prompt: "Which color should the button be?", Labels: []string{"Red"}},
		{Prompt: "Button text", Text: "Go"},
	})
	var ae *AskError
	if !asAskError(err, &ae) || sim.closed {
		t.Fatalf("err %v closed %v", err, sim.closed)
	}
	if last := sim.sent[len(sim.sent)-1]; !reflect.DeepEqual(last, []string{"false", "End", "C-u"}) {
		t.Fatalf("last keys %v, want the field emptied", last)
	}
}

// A required field left empty keeps the dialog open: not applied.
func TestAnswerFormSaysWhenOmpKeepsTheFormOpen(t *testing.T) {
	fastAsk(t)
	sim := colorForm()
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "Which color should the button be?", Labels: []string{"Blue"}}})
	var ae *AskError
	if !asAskError(err, &ae) || sim.closed || sim.released {
		t.Fatalf("err %v closed %v released %v", err, sim.closed, sim.released)
	}
}

// The person at the desk moved to Submit first: the driver goes back.
func TestAnswerFormGoesBackToAnEarlierField(t *testing.T) {
	fastAsk(t)
	sim := colorForm()
	sim.focus = len(sim.stops()) - 1
	sim.fields[1].value = "old words"
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{
		{Prompt: "Which color should the button be?", Labels: []string{"Red"}},
		{Prompt: "Button text", Text: "new words"},
	})
	if err != nil || !sim.closed || sim.released || sim.fields[1].value != "new words" {
		t.Fatalf("err %v closed %v released %v value %q keys %v", err, sim.closed, sim.released, sim.fields[1].value, sim.sent)
	}
}
