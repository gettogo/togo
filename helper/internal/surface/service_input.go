package surface

import (
	"context"
	"encoding/json"
	"slices"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/rpc"
)

func (s *Service) active(ctx context.Context, r *renderer) error {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return errLease
	}
	st, err := s.backend.validate(ctx, r.lease.ID, r.runtime)
	if err != nil {
		return err
	}
	if !st.Native || st.Owner != r.lease.Token {
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
	if err := s.active(ctx, r); err != nil {
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
		err = s.backend.send(ctx, p.ID, r.runtime, wire)
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
	if err := s.active(ctx, r); err != nil {
		return DispatchResult{}, err
	}
	err = r.cache.Inspect(func(scene *Scene) error {
		v := scene.View(p.Surface)
		if v == nil || v.Closed || !v.Listen || v.Instance != p.Instance || v.Document.Suspended {
			return errLease
		}
		d := v.Document
		e := d.nodes[nodeID]
		if e == nil {
			return errLease
		}
		for parent := e; parent != nil; parent = d.nodes[parent.parent] {
			if parent.node.Props["disabled"] == true || parent.node.Props["hidden"] == true {
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
		return nil
	})
	if err != nil {
		return DispatchResult{}, err
	}
	wire, err := Encode('e', p.Event)
	if err == nil {
		err = s.backend.send(ctx, p.ID, r.runtime, wire)
	}
	return DispatchResult{Dispatched: err == nil}, err
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
