package engine

import (
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Seen live on omp 18.20: Effort offered eight levels for a model with no
// adjustable thinking, and picking one was reported as sent while the
// terminal refused it. The choices now come from the active model.
func TestSettingChoicesFollowTheSource(t *testing.T) {
	sc := spec.Screen{Fields: []spec.SettingField{{ID: "effort", Kind: "choice", Options: []string{"off", "low", "high"},
		OptionsFrom: `first(.[] | select(.selector == $state.model)) | if .thinking == null then [] else ["off"] + .thinking + ["auto"] end`,
		None:        "This model has no adjustable thinking level."}}}
	models := []any{
		map[string]any{"selector": "apple/on-device", "thinking": nil},
		map[string]any{"selector": "x/big", "thinking": []any{"low", "medium", "high", "xhigh"}},
	}
	ids := func(model string, data any) ([]string, string) {
		f := buildSettings(sc, data, map[string]string{"model": model}).Sections[0].Fields[0]
		var out []string
		for _, o := range f.Options {
			out = append(out, o.ID)
		}
		return out, f.Help
	}
	if got, _ := ids("x/big", models); !reflect.DeepEqual(got, []string{"off", "low", "medium", "high", "xhigh", "auto"}) {
		t.Errorf("model with levels: %q", got)
	}
	if got, help := ids("apple/on-device", models); len(got) != 0 || help != "This model has no adjustable thinking level." {
		t.Errorf("model without thinking: %q, help %q", got, help)
	}
	// The model is not listed, or the source could not be read: the fixed list.
	if got, _ := ids("other/model", models); !reflect.DeepEqual(got, []string{"off", "low", "high"}) {
		t.Errorf("unlisted model: %q", got)
	}
	if got, _ := ids("x/big", nil); !reflect.DeepEqual(got, []string{"off", "low", "high"}) {
		t.Errorf("no source data: %q", got)
	}
}
