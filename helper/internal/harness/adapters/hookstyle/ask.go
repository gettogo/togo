package hookstyle

import (
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// askStart turns a PreToolUse for the harness's own ask tool (Options.Ask)
// into an open question item, id the call id, so the phone can answer it;
// a plan the call carries (Options.Plan) comes first, id "<call>-plan".
// It returns nothing for any other tool.
func (m *Mapper) askStart(p payload, at time.Time) []harness.Item {
	if m.opts.Ask == nil {
		return nil
	}
	q, ok := m.opts.Ask(p.ToolName, p.ToolInput)
	if !ok || q == nil || len(q.Questions) == 0 {
		return nil
	}
	id := m.callID(p, true)
	m.running[p.ToolName] = append(m.running[p.ToolName], id)
	m.steps[id] = true
	x := harness.Item{ID: id, TurnID: m.turn, Kind: harness.KindQuestion, At: at, Question: q}
	m.asks[id] = x
	if m.opts.Plan != nil {
		if plan, ok := m.opts.Plan(p.ToolName, p.ToolInput); ok && plan != "" {
			return []harness.Item{{ID: id + "-plan", TurnID: m.turn, Kind: harness.KindAssistant, At: at,
				Assistant: &harness.TextBody{Text: Clip(plan)}}, x}
		}
	}
	return []harness.Item{x}
}

// askEnd closes the ask a PostToolUse is about with the answers its
// response carries (Options.Answered); a failed call, or a response it
// cannot read, closes every question with none.
func (m *Mapper) askEnd(p payload, failed bool) (harness.Item, bool) {
	id := m.callID(p, false)
	x, ok := m.asks[id]
	if !ok {
		return harness.Item{}, false
	}
	ids := m.running[p.ToolName]
	for i, v := range ids {
		if v == id {
			m.running[p.ToolName] = append(ids[:i:i], ids[i+1:]...)
			break
		}
	}
	delete(m.asks, id)
	var got map[string][]string
	if !failed && m.opts.Answered != nil {
		got = m.opts.Answered(p.ToolName, p.ToolInput, p.ToolResponse)
	}
	return answered(x, got), true
}

// closeAsks closes every ask still open when the turn ends: it was
// cancelled or answered where no hook saw it.
func (m *Mapper) closeAsks() []harness.Item {
	var out []harness.Item
	for id, x := range m.asks {
		out = append(out, answered(x, nil))
		delete(m.asks, id)
	}
	return out
}

// answered is x with Answered set: got's answers, and [] for every
// question got does not answer, so the item reads as closed.
func answered(x harness.Item, got map[string][]string) harness.Item {
	b := *x.Question
	b.Answered = map[string][]string{}
	for _, q := range b.Questions {
		v := got[q.ID]
		if v == nil {
			v = []string{}
		}
		b.Answered[q.ID] = v
	}
	x.Question = &b
	return x
}
