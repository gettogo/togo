package engine

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Screen reads a screen's source and fills its primitive.
func (e *Engine) Screen(ctx context.Context, id, name string, query map[string]string) (harness.Screen, error) {
	s, err := e.served(ctx, id)
	if err != nil {
		return harness.Screen{}, err
	}
	sc, ok := s.ad.Screens[name]
	if !ok {
		return harness.Screen{}, harness.ErrUnknownScreen
	}
	s.mu.Lock()
	gated := sc.Capability != "" && (s.degraded[sc.Capability] || !harness.Has(s.ad.Capabilities, sc.Capability))
	s.mu.Unlock()
	if gated {
		return harness.Screen{}, &harness.FallbackError{Reason: "source", Detail: "capability is off"}
	}
	return e.buildScreen(ctx, s, name, sc, query)
}

func (e *Engine) buildScreen(ctx context.Context, s *sess, name string, sc spec.Screen, query map[string]string) (harness.Screen, error) {
	var data any
	stale := false
	if sc.Source != "" && sc.Primitive != harness.PrimStatus {
		var err error
		data, stale, err = e.fetch(ctx, s, sc.Source, query, false)
		if err != nil {
			return harness.Screen{}, &harness.FallbackError{Reason: "source", Detail: name}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.stateLocked()
	out := harness.Screen{ID: name, Primitive: sc.Primitive, Title: sc.Title, FetchedAt: e.cfg.Now(), Stale: stale, Actions: []harness.ActionRef{}}
	switch sc.Primitive {
	case harness.PrimPicker:
		p, err := buildPicker(sc, data, state)
		if err != nil {
			return harness.Screen{}, &harness.FallbackError{Reason: "parse", Detail: name}
		}
		p.ItemActions = s.actionRefsLocked(sc.Actions)
		out.Picker = p
	case harness.PrimSettings:
		out.Settings = buildSettings(sc, data, state)
		out.Actions = s.actionRefsLocked(sc.Actions)
	case harness.PrimChecklist:
		out.Checklist = s.buildChecklistLocked(sc, data, state)
		out.Actions = s.actionRefsLocked(sc.Actions)
	case harness.PrimLiveList:
		out.LiveList = &harness.LiveList{Rows: buildRows(sc, data, state)}
		if m, ok := s.ad.Mission[sc.Section]; ok && m.Refresh > 0 {
			out.LiveList.RefreshSeconds = int(m.Refresh.Seconds())
		}
		out.Actions = s.actionRefsLocked(sc.Actions)
	case harness.PrimDetail:
		out.Detail = buildDetail(sc, data, state)
		out.Detail.Actions = s.actionRefsLocked(sc.Actions)
	case harness.PrimConfirm:
		c := &harness.Confirm{Questions: []harness.Question{}, Submit: sc.Submit}
		if act, ok := s.ad.Actions[sc.Submit]; ok {
			c.Message = act.Confirm
			c.Destructive = act.Role == harness.RoleDestructive
		}
		out.Confirm = c
		out.Actions = s.actionRefsLocked(sc.Actions)
	case harness.PrimStatus:
		sec := s.mission.section(harness.SectionStatus)
		if sec == nil || sec.Status == nil {
			return harness.Screen{}, &harness.FallbackError{Reason: "source", Detail: name}
		}
		st := *sec.Status
		st.Actions = s.actionRefsLocked(sc.Actions)
		out.Status = &st
	default:
		return harness.Screen{}, &harness.FallbackError{Reason: "parse", Detail: name}
	}
	return out, nil
}

func buildPicker(sc spec.Screen, data any, state map[string]string) (*harness.Picker, error) {
	list, _ := data.([]any)
	p := &harness.Picker{Items: []harness.PickerItem{}, Searchable: sc.Search, OnSelect: sc.OnSelect}
	facetVals := map[string]map[string]bool{}
	for _, elem := range list {
		get := func(key, def string) string {
			expr := sc.Map["item."+key]
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
			Title:    get("title", ".title // .label // .name // .id"),
			Subtitle: get("subtitle", ""),
			Badge:    get("badge", ""),
			Group:    get("group", ""),
		}
		it.Title = clipRunes(it.Title, harness.MaxTitle)
		it.Subtitle = clipRunes(it.Subtitle, harness.MaxTitle)
		if it.ID == "" {
			continue
		}
		for _, f := range sc.Filters {
			if f.Field == "" {
				continue
			}
			v, _ := MapValue("."+f.Field, elem, state)
			if v == "" {
				continue
			}
			if it.Facets == nil {
				it.Facets = map[string]string{}
			}
			it.Facets[f.ID] = v
			if facetVals[f.ID] == nil {
				facetVals[f.ID] = map[string]bool{}
			}
			facetVals[f.ID][v] = true
		}
		p.Items = append(p.Items, it)
	}
	for _, f := range sc.Filters {
		flt := harness.Filter{ID: f.ID, Label: f.Label, Kind: f.Kind, Default: f.Default}
		if f.Kind == harness.FilterChoice {
			vals := make([]string, 0, len(facetVals[f.ID]))
			for v := range facetVals[f.ID] {
				vals = append(vals, v)
			}
			sort.Strings(vals)
			for _, v := range vals {
				flt.Options = append(flt.Options, harness.Choice{ID: v, Label: v})
			}
		}
		p.Filters = append(p.Filters, flt)
	}
	if sel := sc.Map["selected"]; sel != "" {
		v, err := MapValue(sel, data, state)
		if err == nil {
			p.Selected = v
		}
	}
	return p, nil
}

func buildSettings(sc spec.Screen, data any, state map[string]string) *harness.Settings {
	sec := harness.SettingsSection{Fields: []harness.Field{}}
	for _, f := range sc.Fields {
		fld := harness.Field{ID: f.ID, Label: f.Label, Help: f.Help, Kind: f.Kind, Action: f.Action, Value: json.RawMessage("null")}
		var v any
		if f.Value != "" {
			v, _ = JQFirst(f.Value, data, state)
		}
		if f.Kind == harness.FieldSecret {
			// The value says whether a secret exists: a flag such as
			// keySet, or the (already redacted) value itself.
			if b, ok := v.(bool); ok {
				fld.IsSet = b
			} else {
				fld.IsSet = v != nil && toString(v) != ""
			}
		} else if v != nil {
			if b, err := json.Marshal(v); err == nil {
				fld.Value = b
			}
		}
		for _, o := range f.Options {
			fld.Options = append(fld.Options, harness.Choice{ID: o, Label: titleCase(o)})
		}
		sec.Fields = append(sec.Fields, fld)
	}
	return &harness.Settings{Sections: []harness.SettingsSection{sec}}
}

// buildChecklistLocked fills a checklist from its source, or from the
// mission plan when it has none.
func (s *sess) buildChecklistLocked(sc spec.Screen, data any, state map[string]string) *harness.Checklist {
	c := &harness.Checklist{Items: []harness.CheckItem{}}
	if sc.Source == "" {
		if sec := s.mission.section(harness.SectionPlan); sec != nil {
			for _, r := range sec.Rows {
				c.Items = append(c.Items, harness.CheckItem{ID: r.ID, Text: r.Title, State: r.State})
			}
		}
	} else {
		for _, r := range buildRows(sc, data, state) {
			c.Items = append(c.Items, harness.CheckItem{ID: r.ID, Text: r.Title, State: r.State})
		}
	}
	for _, it := range c.Items {
		c.Total++
		if it.State == harness.RowDone || it.State == harness.RowSkipped {
			c.Done++
		}
	}
	return c
}

func buildRows(sc spec.Screen, data any, state map[string]string) []harness.Row {
	list, _ := data.([]any)
	rows := []harness.Row{}
	for _, elem := range list {
		get := func(key, def string) string {
			expr := sc.Map["item."+key]
			if expr == "" {
				expr = def
			}
			v, _ := MapValue(expr, elem, state)
			return v
		}
		r := harness.Row{ID: get("id", ".id"), Title: get("title", ".title // .name // .id"),
			Subtitle: get("subtitle", ".subtitle // empty"), State: get("state", ".state // .status // empty")}
		if r.ID == "" {
			continue
		}
		rows = append(rows, r)
	}
	return rows
}

func buildDetail(sc spec.Screen, data any, state map[string]string) *harness.Detail {
	d := &harness.Detail{Body: harness.Body{Kind: harness.BodyText}, Actions: []harness.ActionRef{}}
	if k := sc.Map["body.kind"]; k != "" {
		d.Body.Kind = k
	}
	if expr := sc.Map["body.text"]; expr != "" {
		d.Body.Text, _ = MapValue(expr, data, state)
	} else if str, ok := data.(string); ok {
		d.Body.Text = str
	}
	if expr := sc.Map["subtitle"]; expr != "" {
		d.Subtitle, _ = MapValue(expr, data, state)
	}
	return d
}

// actionRefsLocked lists declared actions as refs.
func (s *sess) actionRefsLocked(names []string) []harness.ActionRef {
	out := []harness.ActionRef{}
	for _, n := range names {
		act, ok := s.ad.Actions[n]
		if !ok {
			continue
		}
		role := act.Role
		if role == "" {
			role = harness.RoleNormal
		}
		label := act.Label
		if label == "" {
			label = titleCase(strings.ReplaceAll(n, "_", " "))
		}
		out = append(out, harness.ActionRef{ID: n, Label: label, Role: role, Confirm: act.Confirm})
	}
	return out
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
