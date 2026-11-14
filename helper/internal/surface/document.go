package surface

import (
	"encoding/json"
	"errors"
	"unicode"
	"unicode/utf8"
)

// Document limits bound a pane's native UI independently of its producer.
const (
	MaxDocumentBytes = 8 << 20
	MaxNodes         = 16384
	MaxDepth         = 64
	MaxJSONDepth     = 256 // Child arrays and the frame envelope add nesting.
	MaxOperations    = 4096
	MaxIDBytes       = 4096
)

var errDocument = errors.New("surface: invalid document frame")
var errOperation = errors.New("surface: invalid document operation")

// Node is a TSP document node. Props remain data, never executable markup.
type Node struct {
	ID       string         `json:"id"`
	Kind     string         `json:"k"`
	Props    map[string]any `json:"p,omitempty"`
	Children []*Node        `json:"c,omitempty"`
}

type entry struct {
	node   *Node
	parent string
	depth  int
	cost   int
}

// Document is the state of one surface. Its owner serializes all access.
// Sequence echoes the producer; Revision orders snapshots within this owner.
type Document struct {
	Root      *Node  `json:"root"`
	Sequence  int64  `json:"sequence"`
	Revision  uint64 `json:"revision"`
	Focus     string `json:"focus,omitempty"`
	Suspended bool   `json:"suspended"`

	nodes    map[string]*entry
	kinds    map[string]bool
	bytes    int
	maxBytes int
}

// OperationError identifies a rejected operation without including remote text.
type OperationError struct {
	Index  int    `json:"op"`
	Reason string `json:"msg"`
}

// NewDocument opens an empty root using exactly the negotiated node vocabulary.
func NewDocument(id string, kinds []string) (*Document, error) {
	if !validID(id) {
		return nil, errDocument
	}
	d := &Document{Root: &Node{ID: id, Kind: "col"}, kinds: map[string]bool{}, nodes: map[string]*entry{}, maxBytes: MaxDocumentBytes}
	for _, k := range kinds {
		d.kinds[k] = true
	}
	if !d.kinds["col"] {
		return nil, errDocument
	}
	cost := nodeCost(d.Root)
	d.nodes[id] = &entry{node: d.Root, cost: cost}
	d.bytes = cost
	return d, nil
}

// Apply processes operations in order and draws no intermediate state. A bad
// operation is rejected alone; a malformed frame leaves the document untouched.
func (d *Document) Apply(raw []byte) ([]OperationError, error) {
	if len(raw) > MaxMessageBytes || !jsonDepth(raw, MaxJSONDepth) || !utf8.Valid(raw) {
		return nil, errDocument
	}
	var frame struct {
		Surface  string            `json:"sf"`
		Sequence *int64            `json:"s"`
		Ops      []json.RawMessage `json:"ops"`
	}
	if json.Unmarshal(raw, &frame) != nil || frame.Surface != d.Root.ID || frame.Sequence == nil ||
		*frame.Sequence < 0 || frame.Ops == nil || len(frame.Ops) > MaxOperations {
		return nil, errDocument
	}
	var rejected []OperationError
	for i, rawOp := range frame.Ops {
		if err := d.applyOperation(rawOp); err != nil {
			rejected = append(rejected, OperationError{Index: i, Reason: err.Error()})
		}
	}
	d.Sequence = *frame.Sequence
	d.Revision++
	return rejected, nil
}

func validID(id string) bool {
	if id == "" || len(id) > MaxIDBytes || !utf8.ValidString(id) {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// The extra bytes account conservatively for child delimiters and separators,
// including nodes whose empty children are omitted from their JSON encoding.
func nodeCost(n *Node) int {
	b, _ := json.Marshal(Node{ID: n.ID, Kind: n.Kind, Props: n.Props})
	return len(b) + 9
}

func (d *Document) prepareTree(n *Node, parent string, depth int) (map[string]*entry, int, error) {
	fresh := map[string]*entry{}
	bytes := 0
	var walk func(*Node, string, int) error
	walk = func(n *Node, parent string, depth int) error {
		if n == nil || !validID(n.ID) || !d.kinds[n.Kind] || d.nodes[n.ID] != nil || fresh[n.ID] != nil {
			return errOperation
		}
		if depth > MaxDepth || len(d.nodes)+len(fresh)+1 > MaxNodes {
			return ErrLimit
		}
		cost := nodeCost(n)
		bytes += cost
		if d.bytes+bytes > d.maxBytes {
			return ErrLimit
		}
		fresh[n.ID] = &entry{node: n, parent: parent, depth: depth, cost: cost}
		for _, child := range n.Children {
			if err := walk(child, n.ID, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	err := walk(n, parent, depth)
	return fresh, bytes, err
}

func (d *Document) setProps(e *entry, props map[string]any) error {
	next := make(map[string]any, len(e.node.Props)+len(props))
	for k, v := range e.node.Props {
		next[k] = v
	}
	for k, v := range props {
		if v == nil {
			delete(next, k)
		} else {
			next[k] = v
		}
	}
	cost := nodeCost(&Node{ID: e.node.ID, Kind: e.node.Kind, Props: next})
	if d.bytes-e.cost+cost > d.maxBytes {
		return ErrLimit
	}
	d.bytes += cost - e.cost
	e.cost, e.node.Props = cost, next
	return nil
}

func jsonDepth(data []byte, limit int) bool {
	depth := 0
	quoted, escaped := false, false
	for _, b := range data {
		if quoted {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				quoted = false
			}
			continue
		}
		switch b {
		case '"':
			quoted = true
		case '{', '[':
			depth++
			if depth > limit {
				return false
			}
		case '}', ']':
			depth--
		}
	}
	return true
}
