package appinventory

import (
	"slices"
	"testing"
)

func TestAppMatrix(t *testing.T) {
	inventory := Inventory{Variants: []Variant{
		{ID: "opencode", Agents: []string{"opencode"}, Capabilities: []string{"chat", "commands"}, Screens: []Screen{{ID: "model", Primitive: "picker"}}},
		{ID: "omp", Agents: []string{"omp"}, Capabilities: []string{"chat"}},
		{ID: "auggie", Agents: []string{"auggie"}},
	}, WithoutAdapter: []string{"kiro"}}
	keys, err := MatrixKeys(inventory)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"opencode/app/opencode:model:picker:duoFolded:ax3",
		"opencode/app/opencode:terminal.prompt:settings:iPadNarrow:ax5",
		"omp/app/omp:surface:native:iPhone17:default",
		"omp/app/omp:question:chat:duoBook:ax3",
		"terminal/app/generic:screen-prompt:confirm:watchSmall:default",
	} {
		if !slices.Contains(keys, key) {
			t.Errorf("missing %s", key)
		}
	}
	for _, key := range keys {
		if key == "" || key[:3] == "aug" || key[:3] == "kir" || key == "opencode/app/opencode:model:picker:watchSmall:default" {
			t.Errorf("inapplicable key %q", key)
		}
	}
	if !slices.IsSorted(keys) || len(slices.Compact(slices.Clone(keys))) != len(keys) {
		t.Fatal("matrix keys must be sorted and unique")
	}
}

func TestAppRatchet(t *testing.T) {
	required := []string{"a/app/first", "a/app/next"}
	base := []string{"a/app/first", "a/app/next"}
	passed := []Proof{{Key: required[0], Revision: "current", Tier: ReplayUI, Result: "passed"}}
	if err := CheckRatchet(required, required[1:], base, passed, "current"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		list, base []string
		proof      []Proof
	}{
		{"new debt", required[1:], required[:1], passed},
		{"stale listed key", required, base, passed},
		{"duplicate list entry", []string{required[1], required[1]}, base, passed},
		{"synthetic only", required[1:], base, []Proof{{Key: required[0], Revision: "current", Tier: "synthetic", Result: "passed"}}},
		{"skip", required[1:], base, []Proof{{Key: required[0], Revision: "current", Tier: ReplayUI, Result: "skipped"}}},
		{"other revision", required[1:], base, []Proof{{Key: required[0], Revision: "old", Tier: ReplayUI, Result: "passed"}}},
		{"retired key", required, base, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := required
			if tc.name == "retired key" {
				keys = required[:1]
			}
			if err := CheckRatchet(keys, tc.list, tc.base, tc.proof, "current"); err == nil {
				t.Fatal("invalid matrix passed")
			}
		})
	}
}
