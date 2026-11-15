package surface

import (
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

// keepResult records a node that is about to be removed, if it is a result
// in the transcript: a toast, or a node the program gives a notice role,
// standing in the transcript outside any dialog and holding no field.
func (d *Document) keepResult(e *entry) {
	if !d.retain || e == nil || !d.inTranscript(e) || holdsField(e.node) {
		return
	}
	role, _ := e.node.Props["role"].(string)
	if e.node.Kind != "toast" && !strings.Contains(role, ".notice.") {
		return
	}
	tone, _ := e.node.Props["tone"].(string)
	var b strings.Builder
	nodeText(e.node, &b)
	text, cut := b.String(), false
	if len(text) > maxResultText {
		text, cut = text[:maxResultText], true
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	d.recent.add(RecentResult{ID: e.node.ID, Born: e.born, Gone: d.applying, Kind: e.node.Kind, Role: role, Tone: tone, Text: text, Cut: cut})
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

// nodeText joins what a node and the nodes inside it say, in order.
func nodeText(n *Node, b *strings.Builder) {
	if b.Len() > maxResultText {
		return
	}
	for _, key := range []string{"text", "spans", "md"} {
		switch v := n.Props[key].(type) {
		case string:
			writePart(b, v)
		case []any:
			for _, span := range v {
				if m, ok := span.(map[string]any); ok {
					if t, ok := m["t"].(string); ok {
						b.WriteString(t)
					}
				}
			}
			if len(v) > 0 {
				writePart(b, "")
			}
		}
	}
	for _, child := range n.Children {
		nodeText(child, b)
	}
}

func writePart(b *strings.Builder, s string) {
	if s != "" {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), " ") && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte(' ')
		}
		b.WriteString(s)
	}
}

// stamp sets, or clears, what a scene carries only while it is served.
func (s *Scene) stamp(on bool) {
	for _, v := range s.Views {
		d := v.Document
		v.Recent, v.RecentFloor = nil, nil
		if on {
			v.Recent = append([]RecentResult(nil), d.recent.entries...)
			if d.recent.dropped {
				floor := d.recent.floor
				v.RecentFloor = &floor
			}
		}
		for _, e := range d.nodes {
			e.node.Born = 0
			if on {
				e.node.Born = e.born
			}
		}
	}
}
