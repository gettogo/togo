package appinventory

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// These profiles mirror the supported sizes of SnapshotHarness.swift. A
// screen on iOS does not imply a corresponding screen on Watch.
var phoneLayouts = []string{
	"iPhone17", "duoFolded", "duoOpen", "duoTabletop", "duoBook",
	"iPadPro13", "iPadNarrow", "iPadPro13Landscape", "iPadHalf",
}
var watchLayouts = []string{"watchSmall", "watchLarge"}
var textSizes = []string{"default", "ax3", "ax5"}

// MatrixKeys expands static declarations and shared native route families.
// Terminal-only launches share a single row independent of executable name.
func MatrixKeys(inventory Inventory) ([]string, error) {
	var keys []string
	add := func(agent, variant, route, primitive string, layouts []string) {
		for _, layout := range layouts {
			for _, textSize := range textSizes {
				name := strings.Join([]string{url.QueryEscape(variant), url.QueryEscape(route), url.QueryEscape(primitive), layout, textSize}, ":")
				keys = append(keys, agent+"/app/"+name)
			}
		}
	}

	for _, variant := range inventory.Variants {
		if !slices.Contains(variant.Capabilities, harness.CapChat) {
			continue
		}
		for _, agent := range variant.Agents {
			for _, screen := range variant.Screens {
				add(agent, variant.ID, screen.ID, screen.Primitive, phoneLayouts)
			}
			for _, state := range []string{"conversation", "composer", "question", "notice"} {
				add(agent, variant.ID, state, "chat", phoneLayouts)
			}
			// Describe adds terminal.native to commands adapters with a
			// styled screen and typist when the harness version gate passes.
			if slices.Contains(variant.Capabilities, harness.CapCommands) {
				add(agent, variant.ID, "terminal.menu", "picker", phoneLayouts)
				add(agent, variant.ID, "terminal.prompt", "settings", phoneLayouts)
				add(agent, variant.ID, "terminal.panel", "detail", phoneLayouts)
			}
			if agent == "omp" {
				add(agent, variant.ID, "surface", "native", phoneLayouts)
			}
			add(agent, variant.ID, "request", "confirm", watchLayouts)
		}
	}

	add("terminal", "generic", "terminal", "terminal", phoneLayouts)
	add("terminal", "generic", "screen-prompt", "confirm", phoneLayouts)
	add("terminal", "generic", "screen-prompt", "confirm", watchLayouts)
	slices.Sort(keys)
	for i := 1; i < len(keys); i++ {
		if keys[i-1] == keys[i] {
			return nil, fmt.Errorf("duplicate app matrix key %q", keys[i])
		}
	}
	return keys, nil
}
