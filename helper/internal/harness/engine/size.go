package engine

import (
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A frame on the helper's connection is at most 1 MiB. One notification
// carries at most batchBytes of items, and an open or history page at
// most pageBytes, leaving room for the mission snapshot and the envelope.
var (
	batchBytes = 256 << 10
	pageBytes  = 640 << 10
)

func itemSize(it harness.Item) int {
	b, err := json.Marshal(it)
	if err != nil {
		return 0
	}
	return len(b) + 1
}

// chunkItems splits items, in order, into runs of at most max encoded
// bytes; an item larger than max goes alone.
func chunkItems(items []harness.Item, max int) [][]harness.Item {
	var out [][]harness.Item
	var cur []harness.Item
	size := 0
	for _, it := range items {
		n := itemSize(it)
		if len(cur) > 0 && size+n > max {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, it)
		size += n
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// newestFitting keeps the newest items (the tail) that fit in max encoded
// bytes, at least one, and reports whether older ones were dropped.
func newestFitting(items []harness.Item, max int) ([]harness.Item, bool) {
	size := 0
	for i := len(items) - 1; i >= 0; i-- {
		size += itemSize(items[i])
		if size > max && i < len(items)-1 {
			return items[i+1:], true
		}
	}
	return items, false
}

// fitPage keeps what fits in max encoded bytes of a page of history, and
// reports whether anything was left out. The main conversation comes
// first: its newest items that fit are kept, then the newest steps of
// subagents (items with a parent) fill the room that is left. Taken
// newest first without that order, a session whose subagent files were
// read after the conversation gave pages of subagent steps and none of
// the conversation. At least one item is kept; the order is unchanged.
func fitPage(items []harness.Item, max int) ([]harness.Item, bool) {
	sizes := make([]int, len(items))
	total := 0
	for i, it := range items {
		sizes[i] = itemSize(it)
		total += sizes[i]
	}
	if total <= max {
		return items, false
	}
	keep := make([]bool, len(items))
	used, first := 0, len(items)
	for i := len(items) - 1; i >= 0; i-- { // the conversation, newest first
		if items[i].Parent != "" {
			continue
		}
		if used+sizes[i] > max && used > 0 {
			break
		}
		keep[i], first = true, i
		used += sizes[i]
	}
	if first == len(items) {
		first = 0 // no conversation items at all: only steps
	}
	for i := len(items) - 1; i >= first; i-- { // then steps, newest first
		if items[i].Parent == "" {
			continue
		}
		if used+sizes[i] > max && used > 0 {
			break
		}
		keep[i] = true
		used += sizes[i]
	}
	out := make([]harness.Item, 0, len(items))
	for i, it := range items {
		if keep[i] {
			out = append(out, it)
		}
	}
	return out, true
}
