// Package appinventory reads the declarations that determine native screen coverage.
package appinventory

import (
	"fmt"
	"maps"
	"slices"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Variant describes one registered adapter and its declared screens.
type Variant struct {
	ID           string
	Agents       []string
	VersionRange string
	Screens      []Screen
}

// Screen is a stable screen ID and its declared renderer.
type Screen struct {
	ID        string
	Primitive string
}

// Inventory keeps public agents without adapters visible as separate coverage obligations.
type Inventory struct {
	Variants       []Variant
	WithoutAdapter []string
}

// Read inventories the registered specs without probing whether agents are installed.
// Invalid registrations fail the inventory rather than silently dropping obligations.
func Read(public []agents.Agent, files map[string][]byte) (Inventory, error) {
	var out Inventory
	matched := make(map[string]bool)
	for _, id := range slices.Sorted(maps.Keys(files)) {
		adapter, err := spec.Parse(files[id])
		if err != nil {
			return Inventory{}, fmt.Errorf("adapter %q: %w", id, err)
		}
		if adapter.Adapter != id {
			return Inventory{}, fmt.Errorf("adapter %q declares ID %q", id, adapter.Adapter)
		}
		variant := Variant{ID: id, Agents: slices.Clone(adapter.Match.Agents), VersionRange: adapter.Harness.Range}
		slices.Sort(variant.Agents)
		for _, agent := range variant.Agents {
			matched[agent] = true
		}
		for _, screen := range slices.Sorted(maps.Keys(adapter.Screens)) {
			variant.Screens = append(variant.Screens, Screen{ID: screen, Primitive: adapter.Screens[screen].Primitive})
		}
		out.Variants = append(out.Variants, variant)
	}
	for _, agent := range public {
		if !matched[agent.ID] {
			out.WithoutAdapter = append(out.WithoutAdapter, agent.ID)
		}
	}
	slices.Sort(out.WithoutAdapter)
	return out, nil
}
