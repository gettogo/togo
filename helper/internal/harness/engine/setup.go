package engine

import (
	"context"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// pickerSetup fills a picker's "Not set up" section from its own source.
// The section is extra: when its source fails the picker shows without it.
func (e *Engine) pickerSetup(ctx context.Context, s *sess, su *spec.PickerSetup, query map[string]string) []any {
	if su == nil {
		return nil
	}
	data, _, err := e.fetch(ctx, s, su.Source, query, false)
	if err != nil {
		return nil
	}
	list, _ := data.([]any)
	return list
}

// setsUp reports whether action is a picker's setup action.
func setsUp(screens map[string]spec.Screen, action string) bool {
	for _, sc := range screens {
		if sc.Setup != nil && sc.Setup.Action == action {
			return true
		}
	}
	return false
}

// buildSetup maps the setup entries onto items, leaving out any whose id
// or title names a group the picker already lists (that provider is set
// up).
func buildSetup(su *spec.PickerSetup, list []any, p *harness.Picker, state map[string]string) []harness.PickerItem {
	if su == nil || len(list) == 0 {
		return nil
	}
	groups := map[string]bool{}
	for _, it := range p.Items {
		if it.Group != "" {
			groups[it.Group] = true
		}
	}
	seen := map[string]bool{}
	var out []harness.PickerItem
	for _, elem := range list {
		get := func(key, def string) string {
			expr := su.Map["item."+key]
			if expr == "" {
				expr = def
			}
			if expr == "" {
				return ""
			}
			v, _ := MapValue(expr, elem, state)
			return v
		}
		it := harness.PickerItem{
			ID:       get("id", ".id"),
			Title:    clipRunes(get("title", ".title // .label // .name // .id"), harness.MaxTitle),
			Subtitle: clipRunes(get("subtitle", ""), harness.MaxTitle),
		}
		if it.ID == "" || groups[it.ID] || groups[it.Title] || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		out = append(out, it)
	}
	return out
}
