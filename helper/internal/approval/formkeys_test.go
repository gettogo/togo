package approval

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/panelock"
)

// noComposerKeys fails when a key went to omp's composer: after Escape,
// Ctrl+C or Tab on Submit, or after the dialog closed.
func noComposerKeys(t *testing.T, sim *formSim) {
	t.Helper()
	if sim.released || sim.afterShut != 0 || sim.composer != "" {
		t.Fatalf("keys reached the composer: released %v, after close %d, %q (keys %v)", sim.released, sim.afterShut, sim.composer, sim.sent)
	}
	for _, k := range sim.sent {
		if slices.Contains(k, "Escape") || slices.Contains(k, "C-c") {
			t.Fatalf("sent %v", sim.sent)
		}
	}
}

func answerErr(t *testing.T, err error) *AskError {
	t.Helper()
	var ae *AskError
	if !asAskError(err, &ae) {
		t.Fatalf("err = %v, want an AskError", err)
	}
	return ae
}

func TestAnswerFormDrivesEveryFieldReadsItBackAndSubmits(t *testing.T) {
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
	if !sim.closed || !sim.fields[0].checked[1] || sim.fields[1].value != text {
		t.Fatalf("closed %v fields %+v %+v", sim.closed, sim.fields[0], sim.fields[1])
	}
	noComposerKeys(t, sim)
	want := [][]string{
		{"false", "Down"}, {"false", "Space"}, {"false", "Tab"}, {"true", text}, {"false", "Tab"}, // answer
		{"false", "BTab"}, {"false", "BTab"}, {"false", "BTab"}, // back to the first field
		{"false", "Tab"}, {"false", "Tab"}, // read back each field on the way to Submit
		{"false", "Enter"},
	}
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
		{Prompt: "Notes", Keep: true}, // optional, left as it is
		{Prompt: "Which database?", Text: "CockroachDB, 3 nodes"},
	})
	if err != nil {
		t.Fatalf("%v (keys %v)", err, sim.sent)
	}
	f := sim.fields
	if !sim.closed || !reflect.DeepEqual(f[0].checked, map[int]bool{0: false, 2: true}) || f[0].other != "watchOS" ||
		sim.selected(f[1]) != 1 || f[2].value != "" || len(f[3].checked) != 0 || f[3].other != "CockroachDB, 3 nodes" {
		t.Fatalf("closed %v: %+v %+v %+v %+v", sim.closed, f[0], f[1], f[2], f[3])
	}
	noComposerKeys(t, sim)
}

func TestAnswerFormRefusesBeforeAnyKey(t *testing.T) {
	fastAsk(t)
	for _, c := range []struct {
		name    string
		answers []AskAnswer
	}{
		{"line break", []AskAnswer{{Prompt: "Button text", Text: "two\nlines"}}},
		{"tab", []AskAnswer{{Prompt: "Button text", Text: "a\tb"}}},
		{"edge space", []AskAnswer{{Prompt: "Button text", Text: "trailing "}}},
		// Longer than its row: the input would show only its end.
		{"too long to read back", []AskAnswer{{Prompt: "Button text", Text: strings.Repeat("x", 80)}}},
		{"another question", []AskAnswer{{Prompt: "Which size?", Labels: []string{"L"}}}},
	} {
		sim := colorForm()
		st := New(Config{Tmux: sim, Pub: &fakePub{}})
		err := st.AnswerChoice(context.Background(), sess, c.answers)
		if answerErr(t, err); len(sim.sent) != 0 {
			t.Errorf("%s: keys %v", c.name, sim.sent)
		}
	}
	sim := colorForm()
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "Which color should the button be?", Labels: []string{"Purple"}}})
	if ae := answerErr(t, err); !strings.Contains(ae.Msg, "does not offer") || len(sim.sent) != 0 {
		t.Errorf("an option it lacks: err %v, keys %v", err, sim.sent)
	}
}

// Item 1: a field that holds text close to the answer is emptied and the
// answer typed, and one that shows a placeholder equal to the answer is
// typed into.
func TestAnswerFormTypesUnlessTheFieldHoldsExactlyTheAnswer(t *testing.T) {
	fastAsk(t)
	sim := &formSim{fields: []*simField{
		{prompt: "Version", kind: "text", value: "v1.2beta"},
		{prompt: "Tag", kind: "text", placeholder: "latest"},
	}}
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "Version", Text: "v1.2 beta"}, {Prompt: "Tag", Text: "latest"}})
	if err != nil || sim.fields[0].value != "v1.2 beta" || sim.fields[1].value != "latest" || !sim.closed {
		t.Fatalf("err %v fields %+v %+v", err, sim.fields[0], sim.fields[1])
	}
	noComposerKeys(t, sim)
}

// Item 2: words typed into a check-box group's Other at the desk count as
// an answer: they are emptied. A radio's pick that Enter on Other did not
// drop is caught by the read-back and nothing is submitted.
func TestAnswerFormClearsOtherAndReadsBackTheRadio(t *testing.T) {
	fastAsk(t)
	sim := &formSim{fields: []*simField{
		{prompt: "Which platforms?", kind: "checkbox", opts: []string{"iOS", "macOS"}, checked: map[int]bool{}, other: "desk words"},
	}}
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	if err := st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "Which platforms?", Multi: true, Labels: []string{"iOS"}}}); err != nil {
		t.Fatalf("%v (keys %v)", err, sim.sent)
	}
	if sim.fields[0].other != "" || !sim.fields[0].checked[0] || !sim.closed {
		t.Fatalf("%+v", sim.fields[0])
	}
	// The desk typed Other and then picked Red: Red is the answer, and the
	// read-back agrees (omp's rule), so it is sent as it is.
	sim = colorForm()
	sim.fields[0].other, sim.fields[0].checked = "desk words", map[int]bool{0: true}
	sim.fields[1].value = "Go"
	st = New(Config{Tmux: sim, Pub: &fakePub{}})
	if err := st.AnswerChoice(context.Background(), sess, []AskAnswer{
		{Prompt: "Which color should the button be?", Labels: []string{"Red"}}, {Prompt: "Button text", Text: "Go"},
	}); err != nil || !sim.closed {
		t.Fatalf("err %v closed %v", err, sim.closed)
	}
	sim = colorForm()
	sim.keepPick = true
	sim.fields[0].checked = map[int]bool{0: true}
	st = New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{
		{Prompt: "Which color should the button be?", Text: "Teal"}, {Prompt: "Button text", Text: "Go"},
	})
	if ae := answerErr(t, err); !strings.Contains(ae.Msg, "different answer") || sim.closed {
		t.Fatalf("err %v closed %v", err, sim.closed)
	}
	noComposerKeys(t, sim)
}

// Item 3: typed words that never show are taken out only while their
// field has the focus.
func TestAnswerFormRemovesTypingThatDidNotShow(t *testing.T) {
	fastAsk(t)
	sim := colorForm()
	sim.deaf = true
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{
		{Prompt: "Which color should the button be?", Labels: []string{"Red"}},
		{Prompt: "Button text", Text: "Go"},
	})
	if answerErr(t, err); sim.closed {
		t.Fatal("submitted")
	}
	if last := sim.sent[len(sim.sent)-1]; !reflect.DeepEqual(last, []string{"false", "End", "C-u"}) {
		t.Fatalf("last keys %v, want the field emptied", last)
	}
}

// Items 3 and 4: the dialog closed at the desk in the middle of the
// answer. No key may follow it: they would go into omp's composer.
func TestAnswerFormSendsNothingOnceTheDialogCloses(t *testing.T) {
	fastAsk(t)
	for n := 1; n <= 9; n++ {
		sim := colorForm()
		sim.closeAt = n
		st := New(Config{Tmux: sim, Pub: &fakePub{}})
		err := st.AnswerChoice(context.Background(), sess, []AskAnswer{
			{Prompt: "Which color should the button be?", Labels: []string{"Blue"}},
			{Prompt: "Button text", Text: "Go"},
		})
		answerErr(t, err)
		noComposerKeys(t, sim)
	}
}

// Item 5: a required field left empty sends the focus back with omp's
// message; that is a failure that says so.
func TestAnswerFormSaysWhenOmpKeepsTheFormOpen(t *testing.T) {
	fastAsk(t)
	sim := colorForm()
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "Which color should the button be?", Labels: []string{"Blue"}}})
	if ae := answerErr(t, err); !strings.Contains(ae.Msg, "Required") || sim.closed {
		t.Fatalf("err %v closed %v", err, sim.closed)
	}
	noComposerKeys(t, sim)
}

// Item 5: the dialog left the screen, but the agent's record of the
// answer never came: not confirmed. Once it comes, taken.
func TestAnswerFormWaitsForTheRecord(t *testing.T) {
	fastAsk(t)
	sim := colorForm()
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	answers := []AskAnswer{{Prompt: "Which color should the button be?", Labels: []string{"Red"}}, {Prompt: "Button text", Text: "Go"}}
	checks := 0
	err := st.AnswerChoiceOpen(context.Background(), sess, answers, func() bool { checks++; return true })
	if ae := answerErr(t, err); !strings.Contains(ae.Msg, "not recorded") || !sim.closed {
		t.Fatalf("err %v", err)
	}
	sim = colorForm()
	st = New(Config{Tmux: sim, Pub: &fakePub{}})
	checks = 0
	if err := st.AnswerChoiceOpen(context.Background(), sess, answers, func() bool { checks++; return checks < 3 }); err != nil {
		t.Fatalf("err %v", err)
	}
}

// A second device's answer that waited for the pane finds the question
// answered and types nothing.
func TestAnswerFormChecksTheQuestionIsStillOpen(t *testing.T) {
	fastAsk(t)
	sim := colorForm()
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoiceOpen(context.Background(), sess, []AskAnswer{{Prompt: "Which color should the button be?", Labels: []string{"Red"}}}, func() bool { return false })
	if ae := answerErr(t, err); !strings.Contains(ae.Msg, "already answered") || len(sim.sent) != 0 {
		t.Fatalf("err %v keys %v", err, sim.sent)
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
	if err != nil || !sim.closed || sim.fields[1].value != "new words" {
		t.Fatalf("err %v closed %v value %q keys %v", err, sim.closed, sim.fields[1].value, sim.sent)
	}
	noComposerKeys(t, sim)
}

// Only the form leaves a question as it is.
func TestAnswerChoiceRefusesAKeptQuestion(t *testing.T) {
	fastAsk(t)
	sim := newChoiceSim(simQ{"Which color?", false, []string{"Red", "Blue"}})
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{{Prompt: "Which color?", Keep: true}})
	if answerErr(t, err); len(sim.sent) != 0 {
		t.Fatalf("keys %v", sim.sent)
	}
}

// A prompt sent while the form is being answered waits for the pane's one
// lock: its keys come after the whole answer, never between its keys.
func TestAnswerFormAndAPromptDoNotInterleave(t *testing.T) {
	fastAsk(t)
	sim := colorForm()
	var once sync.Once
	prompted := make(chan struct{})
	sim.onKey = func() {
		once.Do(func() {
			go func() {
				defer close(prompted)
				mu := panelock.For(sess) // what session.SendText takes
				mu.Lock()
				defer mu.Unlock()
				_ = sim.SendKeys(context.Background(), "", true, "PROMPT")
			}()
		})
	}
	st := New(Config{Tmux: sim, Pub: &fakePub{}})
	err := st.AnswerChoice(context.Background(), sess, []AskAnswer{
		{Prompt: "Which color should the button be?", Labels: []string{"Red"}}, {Prompt: "Button text", Text: "Go"},
	})
	<-prompted
	if err != nil {
		t.Fatal(err)
	}
	last := len(sim.sent) - 1
	if !reflect.DeepEqual(sim.sent[last], []string{"true", "PROMPT"}) || !reflect.DeepEqual(sim.sent[last-1], []string{"false", "Enter"}) {
		t.Fatalf("keys %v: the prompt came in the middle of the answer", sim.sent)
	}
}
