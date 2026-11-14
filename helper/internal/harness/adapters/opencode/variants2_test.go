package opencode

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"os"
	"time"
)

// Seen live on 2.3.0: the app had no way to choose a model variant (the
// terminal's /variants). A variant is part of the model the session runs
// (Model.Ref: id, providerID, variant), set by the same route as the model,
// and each model lists its own. The Variant picker lists the current
// model's, with Default first as the terminal does, and a pick switches the
// session's model to that variant.
//
// testdata/v2/models-variants.json is three of the models a 2.2.5 server
// listed (id, providerID, type, name, variants and enabled of each);
// variant-selected.sse is what it sent when the session's model was
// switched to inkling-2-small-free with variant high.
func TestVariantPickerListsTheCurrentModelsVariants(t *testing.T) {
	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	sc, ok := a.Screens["variant"]
	if !ok || sc.Primitive != "picker" || sc.Source != "variants" || sc.OnSelect != "set_model" {
		t.Fatalf("variant screen %+v", sc)
	}
	list := func(provider, model string) []map[string]any {
		t.Helper()
		st := map[string]string{"provider": provider, "model_id": model}
		v, err := engine.JQFirst(a.Sources["variants"].Select, fixture2(t, "models-variants.json"), st)
		if err != nil {
			t.Fatal(err)
		}
		var out []map[string]any
		for _, it := range v.([]any) {
			out = append(out, it.(map[string]any))
		}
		return out
	}
	items := list("opencode", "inkling-2-small-free")
	var names []string
	for _, it := range items {
		names = append(names, it["name"].(string))
	}
	if got := strings.Join(names, " "); got != "Default none minimal low medium high max" {
		t.Fatalf("variants listed: %s", got)
	}
	// A model without variants, and a session whose model is not known yet,
	// have nothing to pick.
	for _, model := range []string{"big-pickle", ""} {
		if got := list("opencode", model); len(got) != 0 {
			t.Errorf("model %q lists %v", model, got)
		}
	}

	// Picking "high" posts the model the server then reported.
	var sent struct {
		Model map[string]any `json:"model"`
	}
	body, err := engine.Render(a.Actions["set_model"].HTTP.Body, map[string]any{"id": items[5]["id"]})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	var selected struct {
		Data struct {
			Model map[string]any `json:"model"`
		} `json:"data"`
	}
	recs := recordedSSE(t, "testdata/v2/variant-selected.sse")
	if err := json.Unmarshal(recs[0].Data, &selected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent.Model, selected.Data.Model) || sent.Model["variant"] != "high" {
		t.Fatalf("posted %v, the server selected %v", sent.Model, selected.Data.Model)
	}
	// Default is the model without a variant.
	if id := items[0]["id"]; id != `{"id":"inkling-2-small-free","providerID":"opencode"}` {
		t.Errorf("Default is %v", id)
	}

	// The row marked as current follows the variant in state.
	for variant, want := range map[string]any{"high": items[5]["id"], "default": items[0]["id"], "": ""} {
		st := map[string]string{"provider": "opencode", "model_id": "inkling-2-small-free", "model_variant": variant,
			"model_ref": `{"id":"inkling-2-small-free","providerID":"opencode"}`}
		got, err := engine.MapValue(sc.Map["selected"], nil, st)
		if err != nil || got != want {
			t.Errorf("variant %q marks %q (%v), want %q", variant, got, err, want)
		}
	}

	// /variants opens the picker instead of the terminal's dialog.
	for _, c := range a.Commands.Static {
		if c.Name == "variants" && c.Screen != "variant" {
			t.Errorf("/variants: %+v", c)
		}
	}
}

// The mapper reports the variant in use, so the picker marks its row. The
// event is the one recorded from 2.2.5 when a variant was picked.
func TestVariantInUseIsReported(t *testing.T) {
	raw, err := os.ReadFile("testdata/v2/variant-selected.sse")
	if err != nil {
		t.Fatal(err)
	}
	m := NewEvents2(harness.Env{Cwd: "/w"})
	for _, line := range strings.Split(string(raw), "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			m.Map(harness.Record{Source: "events", At: time.Now(), Data: json.RawMessage(data)})
		}
	}
	if v := m.State()["model_variant"]; v == "" || v == "default" {
		t.Errorf("model_variant = %q, want the picked variant", v)
	}
}
