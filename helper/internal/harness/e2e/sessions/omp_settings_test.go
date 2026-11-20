package sessions

import (
	"slices"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// The Compaction screen reads omp's own settings list (`omp config list
// --json`, OMP 18.22.2): automatic compaction is on and the threshold is
// -1, the reserve based default. A change is `omp config set`, and it is
// "applied" only when the list read back says so: a project's settings
// that win over the user's leave the value as it was, and the phone is
// told that, not that it worked.
func TestOMPSettingsFromTheConfigList(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "tape", "omp/capability/settings")
	a := newOMPAgent(t, nil, sessiongen.Turn{Say: 30})
	r := a.r
	r.SettleUntil(support.FileLimit, enginetest.Has(a.l.Truth.Last))
	fields := func() map[string]harness.Field {
		t.Helper()
		sc, err := r.Screen("compaction", nil)
		if err != nil || sc.Settings == nil {
			t.Fatalf("compaction screen: %v %+v", err, sc)
		}
		out := map[string]harness.Field{}
		for _, sec := range sc.Settings.Sections {
			for _, f := range sec.Fields {
				out[f.ID] = f
			}
		}
		return out
	}
	f := fields()
	if got := f["enabled"]; got.Kind != harness.FieldToggle || got.Label != "Compact automatically" || string(got.Value) != "true" {
		t.Errorf("enabled field %+v, want a toggle set on", got)
	}
	if got := f["threshold"]; got.Kind != harness.FieldNumber || got.Label != "Threshold percent" || string(got.Value) != "-1" {
		t.Errorf("threshold field %+v, want the number -1", got)
	}
	sc, _ := r.Screen("compaction", nil)
	if !slices.Contains(actionIDs(sc), "snapcompact") {
		t.Errorf("the screen offers %v, want Snapcompact", actionIDs(sc))
	}

	// omp takes the change but a project's setting wins: not applied.
	res, err := r.Act("set_compaction_enabled", "", map[string]any{"value": false})
	if err != nil || res.Result != harness.ResultFailed || res.Message != "Saved to your omp settings, but a setting in this project overrides it, so nothing changed here." {
		t.Fatalf("set while a project overrides it: %+v %v", res, err)
	}
	// omp applies the change: the same call is applied.
	r.SetConfig("compaction.enabled", false)
	if res, err := r.Act("set_compaction_enabled", "", map[string]any{"value": false}); err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("set when omp applies it: %+v %v", res, err)
	}
	r.SetConfig("compaction.thresholdPercent", 70)
	if res, err := r.Act("set_compaction_threshold", "", map[string]any{"value": 70}); err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("set threshold: %+v %v", res, err)
	}
	want := []string{"config set compaction.enabled false", "config set compaction.enabled false", "config set compaction.thresholdPercent 70"}
	var got []string
	for _, c := range r.CLICalls() {
		if strings.HasPrefix(c, "config ") {
			got = append(got, c)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("omp was run as %q, want %q", got, want)
	}
}

func actionIDs(sc harness.Screen) []string {
	var out []string
	for _, a := range sc.Actions {
		out = append(out, a.ID)
	}
	return out
}
