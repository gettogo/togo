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
