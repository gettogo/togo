package daemon

import (
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/appinventory"
)

func TestAppInventoryRegisteredScreens(t *testing.T) {
	public := append(slices.Clone(agents.Registry), agents.Agent{ID: "future-unregistered"})
	files := harness.AdapterFiles()
	got, err := appinventory.Read(public, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Variants) != len(files) || !slices.Contains(got.WithoutAdapter, "future-unregistered") {
		t.Fatalf("inventory omitted registered variants or an unregistered public agent: %d variants, unmatched %v", len(got.Variants), got.WithoutAdapter)
	}
	seen := make(map[string]bool)
	for i, v := range got.Variants {
		if i != 0 && got.Variants[i-1].ID >= v.ID {
			t.Fatalf("variants out of order at %q", v.ID)
		}
		if v.ID != "opencode" && v.ID != "opencode2" {
			continue
		}
		if !slices.Contains(v.Agents, "opencode") || v.VersionRange == "" || !slices.Contains(v.Screens, appinventory.Screen{ID: "model", Primitive: "picker"}) {
			t.Fatalf("lost version binding or model picker in %+v", v)
		}
		seen[v.ID] = true
	}
	if !seen["opencode"] || !seen["opencode2"] {
		t.Fatal("both OpenCode variants must remain independently addressable")
	}
}

func TestAppInventoryRejectsMalformedAdapter(t *testing.T) {
	if _, err := appinventory.Read(nil, map[string][]byte{"broken": []byte("match: [")}); err == nil {
		t.Fatal("malformed adapter was accepted")
	}
}
