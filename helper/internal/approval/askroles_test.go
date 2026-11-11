package approval

import (
	"context"
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestAskRolesInferredFromWholeLabels(t *testing.T) {
	cases := []struct {
		options []string
		want    []string
	}{
		{[]string{"Approve", "Deny"}, []string{"approve", "deny"}},
		{[]string{"Yes", "No"}, []string{"approve", "deny"}},
		{[]string{" allow ", "REJECT"}, []string{"approve", "deny"}},
		{[]string{"Accept", "Cancel", "Later"}, []string{"approve", "deny", "other"}},
		{[]string{"Continue", "Stop"}, []string{"approve", "deny"}},
		{[]string{"staging", "prod"}, []string{"other", "other"}},
		{[]string{"Yes please", "No way"}, []string{"other", "other"}},
		// One option per role: a second match stays other.
		{[]string{"Yes", "Continue", "No", "Stop"}, []string{"approve", "other", "deny", "other"}},
	}
	for _, c := range cases {
		if got := AskRoles(c.options, "", ""); !slices.Equal(got, c.want) {
			t.Errorf("AskRoles(%q) = %q, want %q", c.options, got, c.want)
		}
	}
}

func TestAskRolesExplicitLabelsOverrideInference(t *testing.T) {
	got := AskRoles([]string{"Yes", "Ship it", "Hold"}, "Ship it", "hold")
	if want := []string{"other", "approve", "deny"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	// Only the role given is overridden; the other is still inferred.
	got = AskRoles([]string{"Deploy", "No"}, "Deploy", "")
	if want := []string{"approve", "deny"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestOpenAskCarriesRolesAndApproveAnswersByRole(t *testing.T) {
	st, _ := newStore(t, &fakeTmux{})
	a, ch := st.OpenAsk(context.Background(), wire.AskParams{Title: "Run make deploy?", Options: []string{"Approve", "Deny"}})
	if a.Options[0].Role != wire.RoleApprove || a.Options[1].Role != wire.RoleDeny {
		t.Fatalf("options %+v", a.Options)
	}
	if err := st.Answer(context.Background(), a.RequestID, wire.DecisionApprove, wire.ByPhone); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got != "Approve" {
		t.Fatalf("ask got %q, want Approve", got)
	}
}

// A plain ask's options get no role, whatever their words: a Yes among
// choices is a choice, never a one-tap approve.
func TestPlainAskHasNoRoles(t *testing.T) {
	st := New(Config{Tmux: &fakeTmux{}, Pub: &fakePub{}})
	a, _ := st.OpenAsk(context.Background(), wire.AskParams{Title: "Ship it?", Options: []string{"Yes", "No", "Later"}, Plain: true})
	for _, o := range a.Options {
		if o.Role != wire.RoleOther {
			t.Fatalf("option %+v has a role", o)
		}
	}
	b, _ := st.OpenAsk(context.Background(), wire.AskParams{Title: "Ship it?", Options: []string{"Yes", "No"}})
	if b.Options[0].Role != wire.RoleApprove || b.Options[1].Role != wire.RoleDeny {
		t.Fatalf("yes/no ask %+v", b.Options)
	}
}
