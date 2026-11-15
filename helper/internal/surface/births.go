package surface

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// A command may answer with a line in the transcript and nothing else: a
// toast that is gone two seconds later, a notice that stays. So that the
// phone can show such a result once, and never one that was there before,
// every node records the frame it was added in, and results removed from
// the transcript are kept for a while. Both exist in memory only, for one
// renderer: neither is journaled or written to the cache.

const (
	maxRecentResults = 64
	maxRecentBytes   = 64 << 10
	maxResultText    = 2048
	recentFrames     = 512 // how many frames a removed result is kept for
	transcriptID     = "main"
)

// RecentResult is what is kept of a result removed from the transcript:
// never the node itself.
type RecentResult struct {
	ID   string `json:"id"`
	Born int64  `json:"b"`
	Gone int64  `json:"gone"`
	Kind string `json:"k"`
	Role string `json:"role,omitempty"`
	Tone string `json:"tone,omitempty"`
	Text string `json:"text"`
	Cut  bool   `json:"cut,omitempty"`
}

type recentLog struct {
	entries []RecentResult
	bytes   int
	// floor is the newest birth among results no longer kept, and whether
	// any was dropped.
	floor   int64
	dropped bool
}

func (l *recentLog) drop(n int) {
	for _, e := range l.entries[:n] {
		l.bytes -= len(e.Text)
		l.floor, l.dropped = max(l.floor, e.Born), true
	}
	l.entries = l.entries[n:]
}

func (l *recentLog) add(e RecentResult) {
	l.entries = append(l.entries, e)
	l.bytes += len(e.Text)
	for len(l.entries) > maxRecentResults || l.bytes > maxRecentBytes {
		l.drop(1)
	}
}

// age forgets results removed more than recentFrames frames ago.
func (l *recentLog) age(sequence int64) {
	n := 0
	for n < len(l.entries) && sequence-l.entries[n].Gone > recentFrames {
		n++
	}
	if n > 0 {
		l.drop(n)
	}
}

// keepResult records a node that is being removed, if it is a result in
// the transcript.
func (d *Document) keepResult(e *entry) {
	if r, ok := d.resultOf(e); ok {
		d.recent.add(r)
	}
}

// resultOf is what is kept of a node if it is a result in the transcript:
// a toast, or a node the program gives a notice role, standing in the
// transcript outside any dialog and holding no field.
func (d *Document) resultOf(e *entry) (RecentResult, bool) {
	if !d.retain || e == nil || !d.inTranscript(e) || holdsField(e.node) {
		return RecentResult{}, false
	}
	role, _ := e.node.Props["role"].(string)
	if e.node.Kind != "toast" && !strings.Contains(role, ".notice.") {
		return RecentResult{}, false
	}
	tone, _ := e.node.Props["tone"].(string)
	var parts []string
	nodeText(e.node, &parts)
	text, cut := strings.Join(parts, "\n"), false
	if len(text) > maxResultText {
		text, cut = text[:maxResultText], true
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return RecentResult{ID: e.node.ID, Born: e.born, Gone: d.applying, Kind: e.node.Kind, Role: role, Tone: tone, Text: text, Cut: cut}, true
}

// inTranscript reports whether a node stands directly in the transcript:
// its parent is the view's transcript column.
func (d *Document) inTranscript(e *entry) bool {
	parent := d.nodes[e.parent]
	return parent != nil && parent.node.ID == transcriptID && parent.parent == d.Root.ID
}

func holdsField(n *Node) bool {
	if n.Kind == "input" || n.Kind == "editor" {
		return true
	}
	for _, child := range n.Children {
		if holdsField(child) {
			return true
		}
	}
	return false
}

// nodeText collects what a node and the nodes inside it say, in order, one
// part per node. A node says one thing: its text if it has any, else its
// spans, else its markdown. A node that gives both text and spans gives
// the same words twice.
func nodeText(n *Node, parts *[]string) {
	size := 0
	for _, p := range *parts {
		size += len(p) + 1
	}
	if size > maxResultText {
		return
	}
	said := ""
	if t, ok := n.Props["text"].(string); ok && t != "" {
		said = t
	} else if spans, ok := n.Props["spans"].([]any); ok && len(spans) > 0 {
		var b strings.Builder
		for _, span := range spans {
			if m, ok := span.(map[string]any); ok {
				if t, ok := m["t"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		said = b.String()
	} else if md, ok := n.Props["md"].(string); ok {
		said = md
	}
	if said != "" {
		*parts = append(*parts, said)
	}
	for _, child := range n.Children {
		nodeText(child, parts)
	}
}

// What a served scene may carry beyond the scene itself.
const (
	serveNothing = iota // the scene as it is held and stored
	serveFloors         // only that results were dropped
	serveBirths         // births too, and no list of removed results
	serveAll            // births and the removed results
)

// stamp sets, or clears, what a scene carries only while it is served.
func (s *Scene) stamp(level int) {
	for _, v := range s.Views {
		d := v.Document
		v.Recent, v.RecentFloor = nil, nil
		if level == serveAll {
			v.Recent = append([]RecentResult(nil), d.recent.entries...)
		}
		if level != serveNothing {
			floor, dropped := d.recent.floor, d.recent.dropped
			if level != serveAll {
				// The list is left out: every result in it counts as dropped.
				for _, e := range d.recent.entries {
					floor, dropped = max(floor, e.Born), true
				}
			}
			if dropped {
				v.RecentFloor = &floor
			}
		}
		for _, e := range d.nodes {
			e.node.Born = 0
			if level >= serveBirths {
				e.node.Born = e.born
			}
		}
	}
}

// served is the scene as the phone reads it. Births and removed results
// are added while they fit in limit bytes; a scene that is itself within
// the limit is never refused because of them: first the list of removed
// results is left out (and said to be, by the floor), then the births.
func (s *Scene) served(limit int) ([]byte, error) {
	defer s.stamp(serveNothing)
	var raw []byte
	var err error
	for level := serveAll; level >= serveNothing; level-- {
		s.stamp(level)
		if raw, err = json.Marshal(s); err != nil || len(raw) <= limit {
			break
		}
	}
	return raw, err
}
