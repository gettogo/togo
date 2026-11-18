package surface

import (
	"encoding/json"
	"errors"
	"slices"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/version"
)

// MaxViews bounds the listening and retained surfaces of one pane.
const MaxViews = 4

// RenderOptions describe the native renderer that owns this pane's lease.
type RenderOptions struct {
	Kinds        []string `json:"kinds"`
	Cols         int      `json:"cols"`
	Dark         bool     `json:"dark"`
	ReduceMotion bool     `json:"reduceMotion"`
}

// View is a program's surface plus the helper's document instance identity.
type View struct {
	ID       string    `json:"id"`
	Mode     string    `json:"mode"`
	Title    string    `json:"title,omitempty"`
	Role     string    `json:"role,omitempty"`
	Listen   bool      `json:"listen"`
	Closed   bool      `json:"closed"`
	Instance uint64    `json:"instance"`
	Document *Document `json:"document"`
	// Recent and RecentFloor are filled only while a scene is served: the
	// results lately removed from this view's transcript, and the newest
	// birth among those no longer kept. Never written to disk.
	Recent      []RecentResult `json:"recent,omitempty"`
	RecentFloor *int64         `json:"recentFloor,omitempty"`
	// Adopting: the program took this view up again after switching
	// renderers and has not yet sent its first frame. That frame adds
	// again the parts it redraws, and each replaces what the view kept.
	Adopting bool `json:"adopting,omitempty"`
}

// Scene contains the current native surfaces, including bounded closed ones
// that the same program can adopt after switching renderers.
type Scene struct {
	Program  string        `json:"program"`
	Features []string      `json:"features"`
	Views    []*View       `json:"views"`
	Revision uint64        `json:"revision"`
	Options  RenderOptions `json:"options"`

	instances uint64
}

// NewScene starts with no negotiated program and no surfaces.
func NewScene(options RenderOptions) *Scene {
	return &Scene{Options: options, Views: []*View{}, Features: []string{}}
}

// Handle updates protocol state and returns only required replies and errors.
// Rendered-frame acknowledgments always come from the owning app separately.
func (s *Scene) Handle(m Message) ([]Message, error) {
	if len(m.Body) > MaxMessageBytes || !jsonDepth(m.Body, MaxJSONDepth) || !utf8.Valid(m.Body) {
		return nil, ErrLimit
	}
	switch m.Verb {
	case 'q':
		return s.hello(m.Body)
	case 'o':
		return s.open(m.Body)
	case 'f':
		var head struct {
			Surface string `json:"sf"`
		}
		if json.Unmarshal(m.Body, &head) != nil {
			return nil, errDocument
		}
		v := s.View(head.Surface)
		if v == nil || v.Closed {
			return []Message{event(map[string]any{"ev": "error", "sf": head.Surface, "msg": "unknown surface"})}, nil
		}
		budget := MaxDocumentBytes
		for _, other := range s.Views {
			if other != v {
				budget -= other.Document.bytes
			}
		}
		v.Document.maxBytes = budget
		v.Document.replace, v.Document.retain = v.Adopting, v.Mode == "inline"
		bad, err := v.Document.Apply(m.Body)
		v.Document.replace = false
		if err == nil {
			v.Adopting = false
		}
		if err != nil {
			return []Message{event(map[string]any{"ev": "error", "sf": v.ID, "msg": "invalid frame"})}, nil
		}
		s.Revision++
		var replies []Message
		for _, rejected := range bad {
			replies = append(replies, event(map[string]any{"ev": "error", "sf": v.ID, "s": v.Document.Sequence, "op": rejected.Index, "msg": rejected.Reason}))
		}
		return replies, nil
	case 'x':
		var close struct {
			ID   string `json:"id"`
			Keep bool   `json:"keep"`
		}
		if json.Unmarshal(m.Body, &close) != nil {
			return nil, errDocument
		}
		if v := s.View(close.ID); v != nil {
			if !close.Keep || v.Mode == "screen" {
				s.Views = slices.DeleteFunc(s.Views, func(v *View) bool { return v.ID == close.ID })
			} else {
				v.Closed = true
			}
			s.Revision++
		}
	}
	return nil, nil
}

// View returns the surface with id, or nil. Its owner must serialize access.
func (s *Scene) View(id string) *View {
	for _, v := range s.Views {
		if v.ID == id {
			return v
		}
	}
	return nil
}

func (s *Scene) hello(raw []byte) ([]Message, error) {
	var q struct {
		Query    string   `json:"q"`
		Versions []int    `json:"v"`
		App      string   `json:"app"`
		Features []string `json:"features"`
	}
	if json.Unmarshal(raw, &q) != nil {
		return nil, errDocument
	}
	if q.Query != "hello" || !slices.Contains(q.Versions, 1) {
		return nil, nil
	}
	if len(q.App) > 128 || len(q.Features) > 64 {
		return nil, ErrLimit
	}
	s.Program = q.App
	// A new renderer handshake may follow a helper crash, so the old close
	// messages may never have reached this cache. Retain inline data for adopt,
	// but no previous view remains live under the new renderer.
	for _, view := range s.Views {
		view.Closed = true
	}
	s.Features = s.Features[:0]
	for _, feature := range []string{"edit", "undo", "send"} {
		if slices.Contains(q.Features, feature) {
			s.Features = append(s.Features, feature)
		}
	}
	s.Revision++
	body, _ := json.Marshal(map[string]any{
		"r": "hello", "v": 1, "term": "togo", "ver": version.Current(), "kinds": s.Options.Kinds,
		"features": []string{"adopt", "reduce-motion"}, "apc": MaxAPCBytes, "credits": 2,
		"cols": s.Options.Cols, "dark": s.Options.Dark,
		"reduceMotion": s.Options.ReduceMotion,
	})
	return []Message{{Verb: 'r', Body: body}}, nil
}

func (s *Scene) open(raw []byte) ([]Message, error) {
	var p struct {
		ID, Mode, Title, Role string
		Adopt                 bool
		Listen                *bool
	}
	if json.Unmarshal(raw, &p) != nil || !validID(p.ID) || len(p.Title) > 4096 || len(p.Role) > 256 {
		return nil, errDocument
	}
	if p.Mode == "" {
		p.Mode = "inline"
	}
	if !slices.Contains([]string{"inline", "screen", "flow"}, p.Mode) {
		return nil, errDocument
	}
	listen := p.Listen == nil || *p.Listen
	if v := s.View(p.ID); p.Adopt && v != nil && v.Mode == "inline" {
		s.instances++
		v.Closed, v.Listen, v.Instance, v.Adopting = false, listen, s.instances, true
		v.Document.recent = recentLog{} // another instance: what the earlier one removed is not its
		s.Revision++
		return nil, nil
	} else if p.Adopt {
		return []Message{event(map[string]any{"ev": "gone", "ids": []string{p.ID}})}, nil
	} else if v != nil && !v.Closed {
		return nil, errors.New("surface: duplicate live surface")
	}
	doc, err := NewDocument(p.ID, s.Options.Kinds)
	if err != nil {
		return nil, err
	}
	s.Views = slices.DeleteFunc(s.Views, func(v *View) bool { return v.ID == p.ID })
	if len(s.Views) >= MaxViews {
		at := slices.IndexFunc(s.Views, func(v *View) bool { return v.Closed })
		if at < 0 {
			return nil, ErrLimit
		}
		s.Views = slices.Delete(s.Views, at, at+1)
	}
	s.instances++
	s.Views = append(s.Views, &View{ID: p.ID, Mode: p.Mode, Title: p.Title, Role: p.Role, Listen: listen, Instance: s.instances, Document: doc})
	s.Revision++
	return nil, nil
}

func event(body any) Message {
	raw, _ := json.Marshal(body)
	return Message{Verb: 'e', Body: raw}
}

// RestoreScene validates stored documents and rebuilds their private indexes.
func RestoreScene(raw []byte, options RenderOptions) (*Scene, error) {
	if len(raw) > MaxDocumentBytes+64*1024 || !jsonDepth(raw, MaxJSONDepth) {
		return nil, ErrLimit
	}
	var s Scene
	if !utf8.Valid(raw) || json.Unmarshal(raw, &s) != nil || len(s.Views) > MaxViews || len(s.Program) > 128 || len(s.Features) > 3 {
		return nil, errDocument
	}
	s.Options = options
	seen := map[string]bool{}
	budget := MaxDocumentBytes
	for _, v := range s.Views {
		if v == nil || v.Document == nil || v.Document.Root == nil || v.Document.Root.ID != v.ID ||
			v.Document.Root.Kind != "col" || !validID(v.ID) || seen[v.ID] || v.Instance == 0 ||
			!slices.Contains([]string{"inline", "screen", "flow"}, v.Mode) || len(v.Title) > 4096 || len(v.Role) > 256 || v.Document.Sequence < 0 {
			return nil, errDocument
		}
		d, err := NewDocument(v.ID, options.Kinds)
		if err != nil {
			return nil, err
		}
		d.nodes, d.bytes, d.maxBytes = map[string]*entry{}, 0, budget
		index, bytes, err := d.prepareTree(v.Document.Root, "", 0)
		if err != nil {
			return nil, err
		}
		d.nodes, d.bytes, d.Root = index, bytes, v.Document.Root
		d.Sequence, d.Revision, d.Focus, d.Suspended = v.Document.Sequence, v.Document.Revision, v.Document.Focus, v.Document.Suspended
		if d.Focus != "" && d.nodes[d.Focus] == nil {
			return nil, errDocument
		}
		v.Document = d
		seen[v.ID] = true
		budget -= bytes
		s.instances = max(s.instances, v.Instance)
	}
	return &s, nil
}
