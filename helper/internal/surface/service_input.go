package surface

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/rpc"
)

func (s *Service) active(r *renderer) error {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return errLease
	}
	return nil
}

// Ack is sent only on the owning app's explicit display acknowledgment.
func (s *Service) Ack(ctx context.Context, owner *rpc.Conn, p AckParams) error {
	r, err := s.lookup(owner, p.ID, p.Lease)
	if err != nil {
		return err
	}
	r.action.Lock()
	defer r.action.Unlock()
	if err := s.active(r); err != nil {
		return err
	}
	err = r.cache.Inspect(func(scene *Scene) error {
		v := scene.View(p.Surface)
		if v == nil || v.Closed || v.Instance != p.Instance || p.Sequence < 0 || p.Sequence > v.Document.Sequence {
			return errLease
		}
		for instance := range r.acks {
			retained := false
			for _, view := range scene.Views {
				retained = retained || view.Instance == instance
			}
			if !retained {
				delete(r.acks, instance)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if previous, ok := r.acks[p.Instance]; ok && p.Sequence <= previous {
		return nil
	}
	wire, err := Encode('e', map[string]any{"ev": "ack", "sf": p.Surface, "s": p.Sequence})
	if err == nil {
		err = s.backend.dispatch(ctx, p.ID, r.runtime, r.lease.Token, wire)
	}
	if err == nil {
		r.acks[p.Instance] = p.Sequence
	}
	return err
}

// Input dispatches only semantic events addressing the current enabled node.
// Dispatch is deliberately separate from confirmation by a later program frame.
func (s *Service) Input(ctx context.Context, owner *rpc.Conn, p InputParams) (DispatchResult, error) {
	r, err := s.lookup(owner, p.ID, p.Lease)
	if err != nil {
		return DispatchResult{}, err
	}
	if len(p.Event) > MaxAPCBytes || !utf8.Valid(p.Event) || !jsonDepth(p.Event, MaxJSONDepth) {
		return DispatchResult{}, ErrLimit
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(p.Event, &fields) != nil || fields == nil {
		return DispatchResult{}, ErrMalformed
	}
	str := func(key string) string { var value string; _ = json.Unmarshal(fields[key], &value); return value }
	ev, nodeID := str("ev"), str("id")
	if str("sf") != p.Surface || !validID(nodeID) || !slices.Contains([]string{"focus", "toggle", "select", "activate", "action", "change", "edit", "undo", "send"}, ev) {
		return DispatchResult{}, ErrMalformed
	}
	r.action.Lock()
	defer r.action.Unlock()
	if err := s.active(r); err != nil {
		return DispatchResult{}, err
	}
	var rewritten json.RawMessage
	var sequence int64
	err = r.cache.Inspect(func(scene *Scene) error {
		v := scene.View(p.Surface)
		if v == nil || v.Closed || !v.Listen || v.Instance != p.Instance || v.Document.Suspended {
			return errLease
		}
		d := v.Document
		sequence = d.Sequence
		e := d.nodes[nodeID]
		if e == nil {
			return errLease
		}
		for parent := e; parent != nil; parent = d.nodes[parent.parent] {
			if blocked(parent.node.Props) {
				return errLease
			}
		}
		if slices.Contains([]string{"edit", "undo", "send"}, ev) {
			if !slices.Contains(scene.Features, ev) || e.node.Props["readOnly"] == true || e.node.Props["readonly"] == true {
				return errLease
			}
		}
		if ev == "send" {
			if e.node.Props["sendable"] != true || d.Focus != nodeID {
				return errLease
			}
			var text string
			if string(fields["text"]) == "null" || json.Unmarshal(fields["text"], &text) != nil {
				return ErrMalformed
			}
			current, ok := e.node.Props["text"].(string)
			if !ok || p.ExpectedText == nil || current != *p.ExpectedText {
				return errLease
			}
		}
		if ev == "edit" {
			return validateEdit(fields, e.node)
		}
		if (ev == "select" || ev == "activate") && fields["item"] != nil {
			// A row is chosen, or opened, through the node that holds it,
			// and must be one of its rows as the program shows them now.
			// A stale row never reaches the program.
			var item string
			if json.Unmarshal(fields["item"], &item) != nil || item == "" || len(item) > MaxIDBytes*8 {
				return ErrMalformed
			}
			if !hasRow(e.node, item) {
				return errLease
			}
		}
		if ev == "action" && str("act") == "togo.query" {
			return validatePickerQuery(fields, e.node)
		}
		if ev == "action" && str("act") == "togo.escape" {
			if !slices.Contains(r.lease.Features, "escape") {
				return ErrMalformed
			}
			within, err := escapeTarget(e.node)
			if err != nil {
				return err
			}
			// The program is told which nodes make up the screen to close,
			// from this helper's own picture of it, never from the app.
			fields["within"], _ = json.Marshal(within)
			rewritten, _ = json.Marshal(fields)
		}
		return nil
	})
	if err != nil {
		return DispatchResult{}, err
	}
	if rewritten != nil {
		p.Event = rewritten
	}
	wire, err := Encode('e', p.Event)
	if err != nil {
		return DispatchResult{}, err
	}
	if sender, ok := s.backend.(sendReceipts); ok && ev == "send" {
		consumed, err := sender.dispatchSend(ctx, p.ID, r.runtime, r.lease.Token, wire)
		result := DispatchResult{Dispatched: err == nil}
		if err == nil {
			result.Sequence = &sequence
		}
		if err == nil && consumed {
			result.Consumed = &consumed
		}
		return result, err
	}
	err = s.backend.dispatch(ctx, p.ID, r.runtime, r.lease.Token, wire)
	if errors.Is(err, ErrRefused) {
		refused := true
		return DispatchResult{Dispatched: true, Refused: &refused, Sequence: &sequence}, nil
	}
	if err != nil {
		return DispatchResult{}, err
	}
	return DispatchResult{Dispatched: true, Sequence: &sequence}, nil
}

// sendReceipts is a backend that can tell whether the program's own editor
// submitted a dispatched send.
type sendReceipts interface {
	dispatchSend(ctx context.Context, id string, run runtimeInfo, owner string, data []byte) (consumed bool, err error)
}

func validatePickerQuery(fields map[string]json.RawMessage, node *Node) error {
	var before, query *string
	if json.Unmarshal(fields["before"], &before) != nil || before == nil ||
		json.Unmarshal(fields["query"], &query) != nil || query == nil || len(*query) > 4096 {
		return ErrMalformed
	}
	current, ok := node.Props["query"].(string)
	if node.Kind != "picker" || !ok || current != *before {
		return errLease
	}
	return nil
}

func validateEdit(fields map[string]json.RawMessage, n *Node) error {
	text, ok := n.Props["text"].(string)
	if !ok {
		text = ""
	}
	var from, to, length, cursor int
	var replacement string
	for key, dest := range map[string]*int{"from": &from, "to": &to, "len": &length, "cursor": &cursor} {
		if string(fields[key]) == "null" || json.Unmarshal(fields[key], dest) != nil {
			return ErrMalformed
		}
	}
	if string(fields["text"]) == "null" || json.Unmarshal(fields["text"], &replacement) != nil {
		return ErrMalformed
	}
	units := utf16.Encode([]rune(text))
	boundary := func(i int) bool {
		return i >= 0 && i <= len(units) && !(i > 0 && i < len(units) && units[i-1] >= 0xd800 && units[i-1] <= 0xdbff && units[i] >= 0xdc00 && units[i] <= 0xdfff)
	}
	if length != len(units) || !boundary(from) || !boundary(to) || to < from {
		return errLease
	}
	next := append(append(append([]uint16{}, units[:from]...), utf16.Encode([]rune(replacement))...), units[to:]...)
	if cursor < 0 || cursor > len(next) || cursor > 0 && cursor < len(next) && utf16.IsSurrogate(rune(next[cursor-1])) && next[cursor] >= 0xdc00 && next[cursor] <= 0xdfff {
		return ErrMalformed
	}
	return nil
}

// maxEscapeNodes bounds the nodes named to the program for one Escape.
const maxEscapeNodes = 512

// escapeTarget checks that a node is a screen the program gave no way to
// close, and lists its nodes. Such a screen holds no composer (a field the
// program sends from), no action marked as its end, and no Cancel or Close
// button:
// one that has any of those is closed by that, and Escape is not for it.
func escapeTarget(n *Node) ([]string, error) {
	var ids []string
	var walk func(*Node) error
	walk = func(n *Node) error {
		if len(ids) >= maxEscapeNodes {
			return ErrLimit
		}
		ids = append(ids, n.ID)
		if n.Props["sendable"] == true {
			return ErrMalformed
		}
		switch actions := n.Props["actions"].(type) {
		case []any:
			for _, a := range actions {
				if m, ok := a.(map[string]any); ok && m["end"] == true {
					return ErrMalformed
				}
			}
		case map[string]any:
			if actions["click"] == "cancel" || actions["click"] == "close" {
				return ErrMalformed
			}
		}
		for _, child := range n.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(n); err != nil {
		return nil, err
	}
	return ids, nil
}

// hasRow reports whether a node holds a row that can be chosen now. A list
// holds its rows as child nodes; a picker holds them in its items, and
// shows those its order names when it gives one. Other kinds of node are
// not judged here: what they take is theirs to say.
func hasRow(n *Node, item string) bool {
	switch n.Kind {
	case "list":
		at := slices.IndexFunc(n.Children, func(child *Node) bool { return child.ID == item })
		return at >= 0 && !blocked(n.Children[at].Props)
	case "picker":
		found := false
		if items, ok := n.Props["items"].([]any); ok {
			for _, it := range items {
				if m, ok := it.(map[string]any); ok && m["id"] == item {
					found = !blocked(m)
				}
			}
		}
		if order, ok := n.Props["order"].([]any); ok && found {
			found = slices.ContainsFunc(order, func(entry any) bool { return entry == item })
		}
		return found
	}
	return true
}

// blocked reports whether a row cannot be chosen: it is hidden, or
// disabled. The program marks a disabled row with true or with the reason
// as text ("Sign in with /login").
func blocked(props map[string]any) bool {
	if props["hidden"] == true || props["disabled"] == true {
		return true
	}
	reason, ok := props["disabled"].(string)
	return ok && reason != ""
}
