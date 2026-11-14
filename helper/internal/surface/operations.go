package surface

import (
	"bytes"
	"encoding/json"
	"slices"
	"unicode/utf16"
)

func (d *Document) applyOperation(raw json.RawMessage) error {
	var a []json.RawMessage
	if json.Unmarshal(raw, &a) != nil || len(a) == 0 {
		return errOperation
	}
	var op string
	if json.Unmarshal(a[0], &op) != nil {
		return errOperation
	}
	if op == "suspend" || op == "resume" {
		if len(a) != 1 {
			return errOperation
		}
		d.Suspended = op == "suspend"
		return nil
	}
	if op == "focus" {
		if len(a) != 2 {
			return errOperation
		}
		var id *string
		if json.Unmarshal(a[1], &id) != nil {
			return errOperation
		}
		if id == nil {
			d.Focus = ""
			return nil
		}
		e := d.nodes[*id]
		if e == nil || (e.node.Kind != "input" && e.node.Kind != "editor") {
			return errOperation
		}
		d.Focus = *id
		return nil
	}
	if len(a) < 2 {
		return errOperation
	}
	var id string
	if json.Unmarshal(a[1], &id) != nil || !validID(id) {
		return errOperation
	}
	if op == "add" {
		return d.add(id, a)
	}
	e := d.nodes[id]
	if e == nil {
		return errOperation
	}
	switch op {
	case "set":
		var props map[string]any
		if len(a) != 3 || json.Unmarshal(a[2], &props) != nil || props == nil {
			return errOperation
		}
		return d.setProps(e, props)
	case "text", "splice":
		return d.text(e, op, a)
	case "move":
		return d.move(e, a)
	case "del":
		if len(a) != 2 || e.parent == "" {
			return errOperation
		}
		removeChild(d.nodes[e.parent].node, id)
		d.forget(e.node)
		return nil
	case "settle":
		if len(a) != 2 {
			return errOperation
		}
		return nil
	case "reveal", "scroll":
		var how string
		if len(a) != 3 || json.Unmarshal(a[2], &how) != nil {
			return errOperation
		}
		allowed := []string{"start", "end", "nearest"}
		if op == "scroll" {
			allowed = []string{"start", "end", "line-up", "line-down", "page-up", "page-down"}
		}
		if !slices.Contains(allowed, how) {
			return errOperation
		}
		// These are view-local hints; the original frame carries them to
		// the renderer rather than accumulating them in persisted state.
		return nil
	default:
		return errOperation
	}
}

func (d *Document) add(id string, a []json.RawMessage) error {
	var parent string
	var before *string
	var node Node
	if len(a) != 5 || json.Unmarshal(a[2], &parent) != nil || json.Unmarshal(a[3], &before) != nil ||
		json.Unmarshal(a[4], &node) != nil || node.ID != id {
		return errOperation
	}
	p := d.nodes[parent]
	if p == nil || !childBefore(p.node, before) {
		return errOperation
	}
	fresh, bytes, err := d.prepareTree(&node, parent, p.depth+1)
	if err != nil {
		return err
	}
	insertChild(p.node, &node, before)
	for id, e := range fresh {
		d.nodes[id] = e
	}
	d.bytes += bytes
	return nil
}

func (d *Document) text(e *entry, op string, a []json.RawMessage) error {
	if !slices.Contains([]string{"text", "md", "code", "ansi", "math", "editor", "input", "shimmer", "el"}, e.node.Kind) {
		return errOperation
	}
	old, ok := e.node.Props["text"].(string)
	if e.node.Props["text"] != nil && !ok {
		return errOperation
	}
	var value string
	if op == "text" {
		var mode string
		if len(a) != 4 || !stringArg(a[2], &mode) || !stringArg(a[3], &value) {
			return errOperation
		}
		switch mode {
		case "append":
			value = old + value
		case "replace":
		default:
			return errOperation
		}
	} else {
		var at, del int
		if len(a) != 5 || !integerArg(a[2], &at) || !integerArg(a[3], &del) || !stringArg(a[4], &value) {
			return errOperation
		}
		units := utf16.Encode([]rune(old))
		if at < 0 || del < 0 || at > len(units) || del > len(units)-at || !utf16Boundary(units, at) || !utf16Boundary(units, at+del) {
			return errOperation
		}
		value = string(utf16.Decode(units[:at])) + value + string(utf16.Decode(units[at+del:]))
	}
	return d.setProps(e, map[string]any{"text": value})
}

func stringArg(raw json.RawMessage, value *string) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, value) == nil
}

func integerArg(raw json.RawMessage, value *int) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && (raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') && json.Unmarshal(raw, value) == nil
}

func utf16Boundary(units []uint16, at int) bool {
	return at == 0 || at == len(units) || units[at] < 0xdc00 || units[at] > 0xdfff
}

func (d *Document) move(e *entry, a []json.RawMessage) error {
	var parent string
	var before *string
	if len(a) != 4 || e.parent == "" || json.Unmarshal(a[2], &parent) != nil || json.Unmarshal(a[3], &before) != nil {
		return errOperation
	}
	p := d.nodes[parent]
	if p == nil || !childBefore(p.node, before) || before != nil && *before == e.node.ID {
		return errOperation
	}
	for ancestor := p; ancestor != nil; ancestor = d.nodes[ancestor.parent] {
		if ancestor == e {
			return errOperation
		}
	}
	var fits func(*Node, int) bool
	fits = func(n *Node, depth int) bool {
		if depth > MaxDepth {
			return false
		}
		for _, child := range n.Children {
			if !fits(child, depth+1) {
				return false
			}
		}
		return true
	}
	if !fits(e.node, p.depth+1) {
		return ErrLimit
	}
	removeChild(d.nodes[e.parent].node, e.node.ID)
	insertChild(p.node, e.node, before)
	e.parent = parent
	var reindex func(*Node, int)
	reindex = func(n *Node, depth int) {
		d.nodes[n.ID].depth = depth
		for _, child := range n.Children {
			reindex(child, depth+1)
		}
	}
	reindex(e.node, p.depth+1)
	return nil
}

func childBefore(parent *Node, before *string) bool {
	return before == nil || slices.ContainsFunc(parent.Children, func(n *Node) bool { return n.ID == *before })
}

func insertChild(parent, child *Node, before *string) {
	at := len(parent.Children)
	if before != nil {
		at = slices.IndexFunc(parent.Children, func(n *Node) bool { return n.ID == *before })
	}
	parent.Children = slices.Insert(parent.Children, at, child)
}

func removeChild(parent *Node, id string) {
	at := slices.IndexFunc(parent.Children, func(n *Node) bool { return n.ID == id })
	parent.Children = slices.Delete(parent.Children, at, at+1)
}

func (d *Document) forget(n *Node) {
	for _, child := range n.Children {
		d.forget(child)
	}
	if d.Focus == n.ID {
		d.Focus = ""
	}
	d.bytes -= d.nodes[n.ID].cost
	delete(d.nodes, n.ID)
}
