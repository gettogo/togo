package detect

import (
	"strings"
	"testing"
)

// omp 18.16's ask dialog: radio rows under a cursor, a description under
// each, an Other row that takes words, and the key-hint footer.
func TestParseChoiceReadsOmpAsk(t *testing.T) {
	c, ok := ParseChoice(fixture(t, "omp/real-ask-single"))
	if !ok {
		t.Fatal("no dialog")
	}
	if c.Prompt != "What should I work on next?" || c.Multi || c.Review || c.Tabs != 0 || c.Cursor != 0 || c.TextRow != 3 {
		t.Fatalf("%+v", c)
	}
	if len(c.Options) != 4 || c.Options[0].Label != "Bug fixes" || !strings.HasPrefix(c.Options[0].Detail, "Find and fix") ||
		c.Options[2].Label != "Refactoring" || !c.Options[3].Text {
		t.Fatalf("options %+v", c.Options)
	}
	moved, _ := ParseChoice(fixture(t, "omp/real-ask-single-moved"))
	if moved.Cursor != 1 {
		t.Fatalf("cursor after a move: %d", moved.Cursor)
	}
}

// A multi-select has check boxes, a tab strip when it is one of several
// questions, and shows which boxes are ticked.
func TestParseChoiceReadsOmpMultiSelectAndTabs(t *testing.T) {
	c, ok := ParseChoice(fixture(t, "omp/real-ask-multi-toggled"))
	if !ok || !c.Multi || c.Tabs != 2 || c.Cursor != 1 {
		t.Fatalf("%+v", c)
	}
	var ticked []string
	for _, o := range c.Options {
		if o.Checked {
			ticked = append(ticked, o.Label)
		}
	}
	if strings.Join(ticked, ",") != "Backend,Frontend" {
		t.Fatalf("ticked %v", ticked)
	}
	second, _ := ParseChoice(fixture(t, "omp/real-ask-tabs-second"))
	if second.Prompt != "How should the feature ship?" || second.Multi || second.Tabs != 2 || second.TextRow != 2 {
		t.Fatalf("second question %+v", second)
	}
	if i, ok := second.OptionIndex("direct release"); !ok || i != 1 {
		t.Fatalf("OptionIndex %d %v", i, ok)
	}
}

// The Review page is one Submit row; the custom-answer field has no list
// at all. Both are waiting prompts with nothing to press.
func TestReviewAndCustomAnswerAreWaitingPrompts(t *testing.T) {
	c, ok := ParseChoice(fixture(t, "omp/real-ask-review"))
	if !ok || !c.Review || c.Prompt != "Review answers" || c.Cursor != 0 {
		t.Fatalf("%+v", c)
	}
	for _, name := range []string{"omp/real-ask-review", "omp/real-ask-custom-answer"} {
		p := mustDetect(t, fixture(t, name))
		if p.Kind != KindWaiting || !p.ReadOnly || len(p.Options) != 0 {
			t.Fatalf("%s: %+v", name, p)
		}
	}
}

// omp's footers are key hints: dropped from a push body, never a prompt.
func TestOmpFootersAreKeyHints(t *testing.T) {
	for _, l := range []string{
		"⏎ select · n note · ↑/↓ move · ⎋ cancel · ⌥H hide",
		"␣ toggle · ⏎ next · ↑/↓ move · ⇥/←/→ question · ⎋ cancel · ⌥H hide",
		"⏎ submit · ↑/↓ scroll · ⎋ cancel · ⌥H hide",
	} {
		if !IsKeyHint(l) {
			t.Errorf("not a key hint: %q", l)
		}
		if !choiceFooter(l) {
			t.Errorf("not a choice footer: %q", l)
		}
	}
	for _, l := range []string{
		"⏎ send   ⇧⏎ newline   ⌃T transcript   ⌃C quit",
		"? for shortcuts",
		"╰─                                      ⇧⇥ to change thinking effort",
	} {
		if choiceFooter(l) {
			t.Errorf("an input line's footer taken for a choice: %q", l)
		}
	}
}
