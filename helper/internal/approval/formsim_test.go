package approval

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gettogo/togo/helper/internal/tmux"
)

// formSim draws omp 19.0.1's form dialog the way the captures in
// detect/testdata/omp/real-form-*.txt show it, and takes keys like its
// HtmlFormDialog (packages/tui/src/html-form/dialog.ts): Up and Down move
// between rows, Tab and Shift+Tab between fields, Space picks a radio or
// flips a box, Enter picks a radio and moves on (on Other it drops the
// radio's pick), Left and Right step a dropdown, typing goes into the
// focused input, End and C-u empty it, Enter on Submit checks the required
// fields and closes the dialog or sends the focus back with "1 to fix".
// Escape, C-c and Tab on Submit hand the keys to the composer: released,
// and what follows is typed into the composer.
type formSim struct {
	mu       sync.Mutex
	fields   []*simField
	focus    int // index into stops()
	closed   bool
	released bool
	fix      string // the validation message after a refused Submit
	sent     [][]string
	composer string // keys that reached omp's composer
	// Faults: typed words are lost; Enter on Other keeps the radio's pick;
	// the dialog closes at the desk after this many key calls.
	deaf      bool
	keepPick  bool
	closeAt   int
	afterShut int // key calls after the dialog closed
}

type simField struct {
	prompt      string
	kind        string // radio, checkbox, select, text, textarea
	opts        []string
	checked     map[int]bool
	other       string
	value       string
	placeholder string
	required    bool
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

// otherChosen is omp's rule: a radio group's Other counts when it holds
// words and no option is picked; a check-box group's when it holds words.
func (s *formSim) otherChosen(f *simField) bool {
	if f.other == "" {
		return false
	}
	if f.kind == "checkbox" {
		return true
	}
	for _, on := range f.checked {
		if on {
			return false
		}
	}
	return true
}

func (s *formSim) CapturePane(context.Context, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	b.WriteString(" Thinking about the form.\n")
	if s.closed {
		b.WriteString("╭─── ? Ask ───╮\n│  answered   │\n╰─────────────╯\n π > ◒ Model\n╰─ " + s.composer + "\n")
		return b.String(), nil
	}
	b.WriteString("╭─ Ask · 0 of " + fmt.Sprint(len(s.fields)) + " answered " + strings.Repeat("─", simWidth-25) + "╮\n")
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
				on := f.checked[o]
				if o == len(f.opts) {
					on = s.otherChosen(f)
					switch {
					case f.other != "":
						label += f.other
					case cur.option == o:
						label += " type your own"
					default:
						label += "type your own"
					}
				}
				mark := map[bool]string{false: "○", true: "◉"}[on]
				if f.kind == "checkbox" {
					mark = map[bool]string{false: "☐", true: "☑"}[on]
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
			v := f.value
			if v == "" && f.placeholder != "" {
				v = " " + f.placeholder
			}
			b.WriteString(boxLine("  │ " + v))
			if f.kind == "textarea" {
				b.WriteString(boxLine("  │"))
			}
			if s.fix != "" {
				b.WriteString(boxLine("    " + s.fix))
			}
		}
	}
	b.WriteString(boxLine(""))
	submit := "  Submit"
	if cur.f < 0 {
		submit = "❯ Submit"
	}
	if s.fix != "" {
		submit += "  1 to fix"
	}
	b.WriteString(boxLine(submit))
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
		if f.value == "" {
			return f.placeholder
		}
		return f.value
	}
	var on []string
	for o, l := range f.opts {
		if f.checked[o] {
			on = append(on, "✓ "+l)
		}
	}
	if s.otherChosen(f) {
		on = append(on, "✓ Other: "+f.other)
	}
	return strings.Join(on, "   ")
}

func (s *formSim) SendKeys(_ context.Context, _ string, literal bool, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, append([]string{fmt.Sprint(literal)}, keys...))
	if s.closed {
		s.afterShut++
	}
	if s.closed || s.released {
		s.composer += strings.Join(keys, "")
		return nil
	}
	defer func() {
		if s.closeAt > 0 && len(s.sent) >= s.closeAt {
			s.closed = true // closed at the desk
		}
	}()
	stops := s.stops()
	for _, k := range keys {
		cur := stops[s.focus]
		var f *simField
		if cur.f >= 0 {
			f = s.fields[cur.f]
		}
		other := f != nil && (f.kind == "radio" || f.kind == "checkbox") && cur.option == len(f.opts)
		input := f != nil && (f.kind == "text" || f.kind == "textarea" || other)
		switch {
		case literal:
			if input && !s.deaf {
				if other {
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
		case k == "C-u" && other:
			f.other = ""
		case k == "C-u" && input:
			f.value = ""
		case (k == "Left" || k == "Right") && f != nil && f.kind == "select":
			o := s.selected(f) + map[string]int{"Left": -1, "Right": 1}[k]
			if o >= 0 && o < len(f.opts) {
				f.checked = map[int]bool{o: true}
			}
		case k == "Space" && f != nil && cur.option >= 0 && cur.option < len(f.opts):
			s.toggle(f, cur.option)
		case k == "Enter" && f == nil:
			for _, g := range s.fields {
				text := g.kind == "text" || g.kind == "textarea"
				if g.required && (text && g.value == "" || !text && s.shown(g) == "") {
					s.fix = "Required"
					for j, st := range stops {
						if st.f >= 0 && s.fields[st.f] == g {
							s.focus = j
							break
						}
					}
					return nil
				}
			}
			s.closed = true
		case k == "Enter":
			if other && f.kind == "radio" && f.other != "" && !s.keepPick {
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
