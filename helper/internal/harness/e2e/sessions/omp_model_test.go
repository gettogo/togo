package sessions

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// ompAgent is omp at its prompt: the session's files, the pane on omp's
// idle screen, omp's config file as the machine has it, and an omp that
// takes /switch and /effort the way omp does, by writing the change to its
// session file.
type ompAgent struct {
	e    support.Env
	o    *sessiongen.OMP
	l    *sessiongen.Layout
	r    *enginetest.Rig
	line []string // lines omp was given
}

func newOMPAgent(t *testing.T, extra map[int]sessiongen.OMPTurn, turns ...sessiongen.Turn) *ompAgent {
	t.Helper()
	a := &ompAgent{e: support.NewEnv(t, time.Hour), o: &sessiongen.OMP{TTY: "ttys042", Extra: extra}}
	conf, err := os.ReadFile("../../adapters/omp/testdata/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(a.e.Home, ".omp", "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), conf, 0o644); err != nil {
		t.Fatal(err)
	}
	a.l, err = sessiongen.Write(a.e.Home, a.o, sessiongen.Script{Seed: 52, Start: a.e.Start, Cwd: a.e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	a.r = a.e.RigWith(t, enginetest.Options{Agent: "omp", NoRedact: true})
	a.r.Term.Show(a.r.Term.MustCapture(ompIdle))
	a.r.Term.OnSubmit = func(text string) {
		a.line = append(a.line, text)
		var err error
		switch cmd, arg, _ := strings.Cut(text, " "); cmd {
		case "/switch":
			err = a.o.Change(a.l, arg, "")
		case "/effort":
			err = a.o.Change(a.l, "", arg)
		}
		if err != nil {
			t.Error(err)
		}
	}
	return a
}

func fieldOptions(f harness.Field) []string {
	var out []string
	for _, o := range f.Options {
		out = append(out, o.ID)
	}
	return out
}

// The model and effort pickers read what omp lists (omp models ls), mark
// the model and effort the session runs, offer the thinking levels of that
// model only, and a choice reaches omp as its own /switch and /effort
// command; the status block follows when omp records the change.
func TestOMPModelAndEffortFromThePhone(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "omp/capability/model", "omp/capability/effort")
	a := newOMPAgent(t, map[int]sessiongen.OMPTurn{1: {Effort: "medium"}}, sessiongen.Turn{Say: 30, Tools: 1, Output: 50})
	r := a.r
	st := r.SettleUntil(support.FileLimit, enginetest.Has(a.l.Truth.Last))
	if sb := support.StatusOf(st); sb.Model != "llama.cpp/qwen3.8-27b" || sb.Effort != "medium" {
		t.Fatalf("status %+v before the change", sb)
	}
	// Building the picker teaches the engine the models' names.

	model, err := r.Screen("model", nil)
	if err != nil || model.Picker == nil {
		t.Fatalf("model screen: %v %+v", err, model)
	}
	p := model.Picker
	if p.Selected != "llama.cpp/qwen3.8-27b" || p.OnSelect != "switch_model" || len(p.Items) < 3 {
		t.Fatalf("model picker: selected %q, on select %q, %d items", p.Selected, p.OnSelect, len(p.Items))
	}
	var kimi *harness.PickerItem
	for i, it := range p.Items {
		if it.ID == "moonshot/kimi-k2-0711-preview" {
			kimi = &p.Items[i]
		}
		if it.ID == "llama.cpp/qwen3.8-27b" && it.Facets["set_role_default"] != "true" {
			t.Errorf("the model holding the default role is not marked: %+v", it)
		}
	}
	if kimi == nil || kimi.Group != "moonshot" || kimi.Title != "Kimi K2 0711" {
		t.Fatalf("the picker lacks omp's own model list: %+v", p.Items)
	}

	// Effort: the levels the active model takes, from omp's model list.
	eff, err := r.Screen("effort", nil)
	if err != nil || eff.Settings == nil {
		t.Fatalf("effort screen: %v %+v", err, eff)
	}
	f := eff.Settings.Sections[0].Fields[0]
	if got := fieldOptions(f); !slices.Equal(got, []string{"off", "low", "medium", "xhigh", "auto"}) || string(f.Value) != `"medium"` {
		t.Fatalf("effort field options %v value %s, want the model's levels at medium", got, f.Value)
	}
	res, err := r.Act("set_effort", "", map[string]any{"value": "xhigh"})
	if err != nil || res.Result != harness.ResultTyped && res.Result != harness.ResultApplied {
		t.Fatalf("set effort: %+v %v", res, err)
	}
	st = r.SettleUntil(support.FileLimit, func(st enginetest.State) bool { return support.StatusOf(st).Effort == "xhigh" })
	if !slices.Equal(a.line, []string{"/effort xhigh"}) {
		t.Errorf("omp was given %q, want /effort xhigh", a.line)
	}

	res, err = r.Act("switch_model", "", map[string]any{"id": kimi.ID})
	if err != nil || res.Result != harness.ResultTyped && res.Result != harness.ResultApplied {
		t.Fatalf("switch model: %+v %v", res, err)
	}
	// The chip and the notice say the name the picker shows.
	st = r.SettleUntil(support.FileLimit, func(st enginetest.State) bool { return support.StatusOf(st).Model == kimi.Title })
	if !slices.Equal(a.line, []string{"/effort xhigh", "/switch " + kimi.ID}) {
		t.Errorf("omp was given %q", a.line)
	}
	if n := support.Items(st, support.Notice(harness.NoticeModel)); len(n) != 1 || n[0].Notice.Text != "Model "+kimi.Title {
		t.Errorf("model notices %+v after the switch, want one naming %s", n, kimi.Title)
	}
	model, _ = r.Screen("model", nil)
	if model.Picker == nil || model.Picker.Selected != kimi.ID {
		t.Errorf("the picker selects %+v after the switch", model.Picker)
	}
	// This model has no adjustable thinking: the effort screen offers none.
	eff, _ = r.Screen("effort", nil)
	if eff.Settings != nil && len(eff.Settings.Sections) > 0 && len(eff.Settings.Sections[0].Fields) > 0 {
		if o := fieldOptions(eff.Settings.Sections[0].Fields[0]); len(o) != 0 {
			t.Errorf("effort levels %v offered for a model with none", o)
		}
	}
}
