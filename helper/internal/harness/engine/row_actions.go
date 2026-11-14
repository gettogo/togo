package engine

import (
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// rowActionsLocked gives each live-list row the actions its item names. A
// screen's item.actions is jq over one source item that lists action
// names, so a row offers what fits its state (an MCP server that is
// connected offers Disconnect, a stopped one Connect). The app runs one
// with the row's id as the target. Names the adapter does not declare are
// left out; a screen without item.actions keeps rows without actions.
func (s *sess) rowActionsLocked(sc spec.Screen, data any, state map[string]string, rows []harness.Row) {
	expr := sc.Map["item.actions"]
	if expr == "" {
		return
	}
	idExpr := sc.Map["item.id"]
	if idExpr == "" {
		idExpr = ".id"
	}
	byID := map[string][]harness.ActionRef{}
	list, _ := data.([]any)
	for _, elem := range list {
		id, _ := MapValue(idExpr, elem, state)
		v, err := JQFirst(expr, elem, state)
		names, _ := v.([]any)
		if id == "" || err != nil || len(names) == 0 {
			continue
		}
		var ids []string
		for _, n := range names {
			if name, ok := n.(string); ok {
				ids = append(ids, name)
			}
		}
		if refs := s.actionRefsLocked(ids); len(refs) > 0 {
			byID[id] = refs
		}
	}
	for i := range rows {
		rows[i].Actions = byID[rows[i].ID]
	}
}
