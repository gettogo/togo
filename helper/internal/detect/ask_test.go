package detect

import (
	"os"
	"reflect"
	"testing"
)

func readAsk(t *testing.T, name string) (Ask, bool) {
	t.Helper()
	b, err := os.ReadFile("testdata/claude/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return ParseAsk(string(b))
}

func TestParseAskTabs(t *testing.T) {
	a, ok := readAsk(t, "ask-tabs")
	if !ok {
		t.Fatal("no dialog")
	}
	want := []AskTab{{Label: "Color"}, {Label: "Fruits"}}
	if !reflect.DeepEqual(a.Tabs, want) {
		t.Errorf("tabs = %+v", a.Tabs)
	}
	if a.Prompt != "Which color do you prefer?" || a.Multi || a.TypeN != 3 || a.Cursor != 1 || a.Review {
		t.Errorf("ask = %+v", a)
	}
	if !reflect.DeepEqual(a.Options, []AskOption{{N: 1, Label: "Red"}, {N: 2, Label: "Blue"}}) {
		t.Errorf("options carry descriptions or extras: %+v", a.Options)
	}
}

func TestParseAskMultiSelect(t *testing.T) {
	a, ok := readAsk(t, "ask-multi")
	if !ok {
		t.Fatal("no dialog")
	}
	if !a.Multi || a.TypeN != 4 || a.Prompt != "Which fruits do you like (pick any)" {
		t.Errorf("ask = %+v", a)
	}
	want := []AskOption{{N: 1, Label: "Apple", Checked: true}, {N: 2, Label: "Pear"}, {N: 3, Label: "Plum", Checked: true}}
	if !reflect.DeepEqual(a.Options, want) {
		t.Errorf("options = %+v", a.Options)
	}
	if !a.Tabs[0].Answered || !a.Tabs[1].Answered {
		t.Errorf("tabs = %+v", a.Tabs)
	}
	if n, ok := a.OptionN("Plum"); !ok || n != 3 {
		t.Errorf("OptionN(Plum) = %d, %v", n, ok)
	}
}

func TestParseAskReview(t *testing.T) {
	a, ok := readAsk(t, "ask-submit")
	if !ok || !a.Review || a.SubmitN != 1 || a.Prompt != "" {
		t.Fatalf("ask = %+v, %v", a, ok)
	}
}

func TestParseAskSingleQuestion(t *testing.T) {
	a, ok := readAsk(t, "ask-single")
	if !ok || len(a.Tabs) != 0 || a.Prompt != "Which color do you prefer?" || len(a.Options) != 2 {
		t.Fatalf("ask = %+v, %v", a, ok)
	}
}

func TestParseAskIgnoresPermissionMenus(t *testing.T) {
	for _, name := range []string{"bash-permission", "edit-permission", "plan-approval", "real-trust-folder"} {
		if a, ok := readAsk(t, name); ok {
			t.Errorf("%s read as a question dialog: %+v", name, a)
		}
	}
}

func TestDetectMarksTabbedAskReadOnly(t *testing.T) {
	for name, ro := range map[string]bool{"ask-tabs": true, "ask-multi": true, "ask-single": false} {
		b, _ := os.ReadFile("testdata/claude/" + name + ".txt")
		p, ok := Detect(string(b))
		if !ok || p.ReadOnly != ro {
			t.Errorf("%s: ok %v readOnly %v, want %v", name, ok, p.ReadOnly, ro)
		}
	}
}
