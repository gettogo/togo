package engine

import (
	"context"

	"github.com/gettogo/togo/helper/internal/harness"
)

// modeList is the mode chip's choices. An adapter whose harness lets the
// user add modes (OpenCode's primary agents) names a source for them; its
// list is served while it answers, in its order. The choices written in the
// adapter are served otherwise: no source, a failed read, an empty list.
// The list is bounded here, since a source's length is the harness's to
// decide: at most MaxChoices modes, labels clipped as every title is.
func (e *Engine) modeList(ctx context.Context, s *sess) []harness.Choice {
	out := []harness.Choice{}
	m := s.ad.Modes
	if m == nil {
		return out
	}
	if m.Source != "" {
		data, _, err := e.fetch(ctx, s, m.Source, nil, false)
		list, _ := data.([]any)
		seen := map[string]bool{}
		for _, el := range list {
			if err != nil || len(out) >= harness.MaxChoices {
				break
			}
			row, _ := el.(map[string]any)
			id := toString(row["id"])
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			label := toString(row["name"])
			if label == "" {
				label = id
			}
			out = append(out, harness.Choice{ID: id, Label: truncate(harness.MaxTitle, label),
				Detail: truncate(harness.MaxTitle, row["description"])})
		}
		if len(out) > 0 {
			return out
		}
	}
	for _, c := range m.Choices {
		out = append(out, harness.Choice{ID: c.ID, Label: c.Label, Detail: c.Detail})
	}
	return out
}
