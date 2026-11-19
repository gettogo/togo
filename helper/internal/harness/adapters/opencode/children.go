package opencode

import "github.com/gettogo/togo/helper/internal/harness"

// maxChildren bounds the child sessions a mapper remembers.
const maxChildren = 1024

// childSet holds every session the server reported with a parent, followed
// or not. A child session is a subagent's: a prompt in it is never the TUI
// moving to another conversation. The zero value is ready to use.
type childSet struct {
	ids   map[string]bool
	order []string
}

func (c *childSet) add(id string) {
	if id == "" || c.ids[id] {
		return
	}
	if c.ids == nil {
		c.ids = map[string]bool{}
	}
	if len(c.order) >= maxChildren {
		delete(c.ids, c.order[0])
		c.order = c.order[1:]
	}
	c.ids[id] = true
	c.order = append(c.order, id)
}

func (c *childSet) has(id string) bool { return c.ids[id] }

func (c *childSet) all() []string { return append([]string(nil), c.order...) }

// Carry gives the fresh mapper the engine made for another conversation
// the child sessions this one saw (harness.Carrier). The server reports a
// session's parent when the session is created, not again, so without
// this a subagent of the session left could take the chat when its
// prompt is updated after the move.
func (e *Events) Carry(next harness.Mapper) {
	n, ok := next.(*Events)
	if !ok || n == e {
		return
	}
	e.mu.Lock()
	ids := e.kids.all()
	e.mu.Unlock()
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, id := range ids {
		n.kids.add(id)
	}
}

// Carry is Events.Carry for the 2.x mapper.
func (e *Events2) Carry(next harness.Mapper) {
	n, ok := next.(*Events2)
	if !ok || n == e {
		return
	}
	e.mu.Lock()
	ids := e.kids.all()
	e.mu.Unlock()
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, id := range ids {
		n.kids.add(id)
	}
}
