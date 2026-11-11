package detect

import (
	"reflect"
	"strings"
	"testing"
)

func labels(c Choice) []string {
	var out []string
	for _, o := range c.Options {
		out = append(out, o.Label)
	}
	return out
}

// Crush 0.100.0 and Kimi Code 2.5.3 dialogs, from real captures: how the
// shared key driver must drive each.
func TestAgentChoices(t *testing.T) {
	cases := []struct {
		fixture string
		want    Choice
		labels  []string
	}{
		{"crush/real-ask-single-80", Choice{Prompt: "Which color should the button be?", Cursor: 0, TextRow: 3, InPlace: true},
			[]string{"Red", "Green", "Blue", "Something else?"}},
		{"crush/real-ask-single-moved", Choice{Prompt: "Which colour should the banner use?", Cursor: 1, TextRow: 3, InPlace: true},
			[]string{"Red", "Green", "Blue", "Something else?"}},
		{"crush/real-ask-multi-toggled", Choice{Prompt: "Which features?", Tabs: 4, Multi: true, Cursor: 2, TextRow: 3, InPlace: true},
			[]string{"Auth", "Billing", "Search", "Something else?"}},
		{"crush/real-ask-yes-no", Choice{Prompt: "Delete the notes file?", Cursor: 1, TextRow: -1, Toggle: true},
			[]string{"Yes", "No"}},
		{"crush/real-ask-free-text", Choice{Prompt: "Any concerns?", Tabs: 4, Cursor: 0, TextRow: 0, InPlace: true},
			[]string{"Type your answer"}},
		{"crush/real-ask-confirm", Choice{Prompt: "Ready to go?", Tabs: 4, Review: true, Cursor: 0, TextRow: -1},
			[]string{"Submit"}},
		{"kimi/real-ask-single", Choice{Prompt: "Which colour should the banner use?", Tabs: 1, Cursor: 0, TextRow: 3, Digits: true, Next: "Tab"},
			[]string{"Red", "Green", "Blue", "Other"}},
		{"kimi/real-ask-other-field", Choice{Prompt: "Which colour should the banner use?", Tabs: 1, Cursor: 3, TextRow: 3, Digits: true, Next: "Tab", Typing: true},
			[]string{"Red", "Green", "Blue", "Other"}},
		{"kimi/real-ask-multi-toggled", Choice{Prompt: "Which features?", Tabs: 2, Multi: true, Cursor: -1, TextRow: 3, Digits: true, Next: "Tab"},
			[]string{"Auth", "Billing", "Search", "Other"}},
		{"kimi/real-ask-review", Choice{Prompt: "Ready to submit your answers?", Tabs: 2, Review: true, Cursor: 0, TextRow: -1, Digits: true, Next: "Tab"},
			[]string{"Submit"}},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			c, ok := ParseChoice(fixture(t, tc.fixture))
			if !ok {
				t.Fatal("not read")
			}
			if got := labels(c); !reflect.DeepEqual(got, tc.labels) {
				t.Fatalf("labels %q", got)
			}
			c.Options = nil
			if !reflect.DeepEqual(c, tc.want) {
				t.Fatalf("got  %+v\nwant %+v", c, tc.want)
			}
		})
	}
	c, _ := ParseChoice(fixture(t, "kimi/real-ask-multi-toggled"))
	if !c.Options[0].Checked || c.Options[1].Checked || !c.Options[2].Checked || c.Options[0].Detail != "Sign-up, sign-in and session management." {
		t.Fatalf("multi rows %+v", c.Options)
	}
	c, _ = ParseChoice(fixture(t, "kimi/real-ask-single"))
	if c.Options[1].Detail != "Calming and natural; suggests growth, success, or eco themes." {
		t.Fatalf("detail %q", c.Options[1].Detail)
	}
}

// A wide pane draws Crush's sidebar to the right of the dialog's lines.
func TestCrushSidebarIgnored(t *testing.T) {
	screen := fixture(t, "crush/real-ask-single-moved")
	var b strings.Builder
	for i, l := range strings.Split(screen, "\n") {
		r := []rune(l)
		pad := 90 - len(r)
		if pad < 2 {
			pad = 2
		}
		b.WriteString(l + strings.Repeat(" ", pad) + []string{"● skill-one", "LSPs ────", "None", "● skill-two"}[i%4] + "\n")
	}
	c, ok := ParseChoice(b.String())
	if !ok || c.Prompt != "Which colour should the banner use?" || c.Cursor != 1 || !reflect.DeepEqual(labels(c), []string{"Red", "Green", "Blue", "Something else?"}) {
		t.Fatalf("%v %+v", ok, c)
	}
}

// A reason typed from the phone first presses the option that opens its
// field: Kimi's "Reject with feedback" and "Revise"; Goose's line field
// takes words straight away.
func TestTextKeys(t *testing.T) {
	for name, want := range map[string][]string{
		"kimi/real-run-command":    {"4"},
		"kimi/real-plan-review":    {"3"},
		"goose/real-elicit-text":   {},
		"goose/real-permission":    nil,
		"crush/real-ask-single-80": nil,
	} {
		p := mustDetect(t, fixture(t, name))
		if !reflect.DeepEqual(p.TextKeys, want) {
			t.Errorf("%s: text keys %#v, want %#v", name, p.TextKeys, want)
		}
	}
}
