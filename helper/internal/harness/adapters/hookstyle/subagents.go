package hookstyle

import (
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A subagent's own hooks. Copilot CLI (1.0.114, seen live) runs a subagent
// as an agent of its own: between the parent's subagentStart and
// subagentStop it fires userPromptSubmitted, tool hooks and agentStop with
// the subagent's id as sessionId and the parent's as parentAgentId. Read as
// the session's own, the subagent's prompt became a prompt of the chat, it
// ended the main turn as interrupted, and its id replaced the session's,
// so the main agent's reply was read from the wrong log.
//
// A hook belongs to a subagent when it names a parent other than its own
// session, or when its session id is an agent a subagentStart announced.
// A different session id with neither is not taken for a subagent: it may
// be a nested run or a /new, which the mapper handles as before.

// maxSubs bounds the subagent ids kept for one session.
const maxSubs = 256

// isSub reports whether p is a subagent's own hook.
func (m *Mapper) isSub(p payload) bool {
	if p.SessionID == "" || p.Event == SubagentStart || p.Event == SubagentStop {
		return false
	}
	if p.ParentAgentID != "" && p.ParentAgentID != p.SessionID {
		return true
	}
	_, ok := m.subs[p.SessionID]
	return ok
}

// noteSub remembers a subagent id a subagentStart announced, as running.
// Ids stay known after the agent stops, so a late hook of a finished
// subagent is still its own.
func (m *Mapper) noteSub(id string) {
	if len(m.subs) >= maxSubs {
		for k, done := range m.subs {
			if done {
				delete(m.subs, k)
			}
		}
		if len(m.subs) >= maxSubs {
			m.subs = map[string]bool{}
		}
	}
	m.subs[id] = false
}

// subHook maps a subagent's own hook. Its prompt and tools are drawn in
// the subagent's conversation (Parent is its agent item); its stop and
// session events do not touch the main turn; the session's own state
// (its id, model, mode) is not learnt from it. Hooks of a subagent that
// has stopped are dropped.
func (m *Mapper) subHook(p payload, at time.Time, draw bool) []harness.Item {
	id := p.SessionID
	if m.subs[id] {
		return nil
	}
	open := m.agents[id]
	var items []harness.Item
	if len(open) == 0 {
		// Never announced: a parentAgentId says it is a subagent. It gets
		// an item of its own, closed by its subagentStop or with the turn.
		a := m.newAgent(id, p.AgentType, at)
		m.agents[id] = []agentRow{a}
		m.subs[id] = false
		open = m.agents[id]
		m.closed = append(m.closed, a.op())
		items = append(items, a.item)
	}
	parent := open[0].item.ID
	switch p.Event {
	case UserPromptSubmit:
		if p.Prompt == "" {
			break
		}
		m.n++
		items = append(items, harness.Item{ID: parent + ":prompt-" + strconv.Itoa(m.n), TurnID: m.turn, Parent: parent,
			Kind: harness.KindUser, At: at, User: &harness.UserBody{Text: Clip(p.Prompt), Source: "unknown"}})
	case PreToolUse:
		if m.turn == "" {
			return nil
		}
		m.working(at)
		x := m.toolStart(p, at)
		x.Parent = parent
		m.tools[x.ID] = x
		items = append(items, x)
	case PostToolUse, PostToolUseFailure:
		if x, ok := m.toolEnd(p, at, p.Event == PostToolUseFailure); ok {
			items = append(items, x)
		}
	case PermissionRequest:
		m.working(at)
		cid := m.callID(p, false)
		if x, ok := m.tools[cid]; ok {
			b := *x.Tool
			b.State = harness.StateWaiting
			x.Tool = &b
			m.tools[cid] = x
			items = append(items, x)
		}
	}
	if !draw {
		return nil
	}
	return items
}

// taskCall is the newest running call of a tool that starts subagents.
func (m *Mapper) taskCall() (harness.Item, bool) {
	var best harness.Item
	found := false
	for _, x := range m.tools {
		if x.Tool == nil || x.Parent != "" || !isTaskTool(x.Tool.Name) {
			continue
		}
		if !found || x.Tool.Started.After(best.Tool.Started) {
			best, found = x, true
		}
	}
	return best, found
}

func isTaskTool(name string) bool {
	switch strings.ToLower(name) {
	case "task", "agent", "subagent":
		return true
	}
	return false
}
