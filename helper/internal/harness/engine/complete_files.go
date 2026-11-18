package engine

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// completeFiles lists files under cwd whose path contains the query,
// files whose name matches first.
func completeFiles(ctx context.Context, cwd, query string, limit int) []harness.Completion {
	q := strings.ToLower(query)
	type hit struct {
		rel  string
		rank int
	}
	var hits []hit
	visited := 0
	_ = filepath.WalkDir(cwd, func(p string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil {
			return filepath.SkipDir
		}
		visited++
		if visited > maxWalk {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if p != cwd && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || isJunk(d.Name()) {
			return nil
		}
		rel, _ := filepath.Rel(cwd, p)
		lr := strings.ToLower(rel)
		switch {
		case strings.Contains(strings.ToLower(d.Name()), q):
			hits = append(hits, hit{rel, 0})
		case strings.Contains(lr, q):
			hits = append(hits, hit{rel, 1})
		}
		return nil
	})
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		if len(hits[i].rel) != len(hits[j].rel) {
			return len(hits[i].rel) < len(hits[j].rel)
		}
		return hits[i].rel < hits[j].rel
	})
	var out []harness.Completion
	for _, h := range hits {
		if len(out) >= limit {
			break
		}
		dir := filepath.Dir(h.rel)
		if dir == "." {
			dir = ""
		}
		out = append(out, harness.Completion{Insert: "@" + h.rel + " ", Title: filepath.Base(h.rel), Detail: dir})
	}
	return out
}

// shareLimit joins the lists in order, at most limit items in all. When
// they do not all fit, each list keeps an equal share of its best items and
// room a short list leaves goes to the others, so a folder with many
// matches cannot push another folder's files out of the result.
func shareLimit(lists [][]harness.Completion, limit int) []harness.Completion {
	take := make([]int, len(lists))
	for left := limit; left > 0; {
		open := 0
		for i, l := range lists {
			if take[i] < len(l) {
				open++
			}
		}
		if open == 0 {
			break
		}
		share := max(left/open, 1)
		for i, l := range lists {
			n := min(share, len(l)-take[i], left)
			take[i] += n
			left -= n
		}
	}
	var out []harness.Completion
	for i, l := range lists {
		out = append(out, l[:take[i]]...)
	}
	return out
}

// mentionForm writes each file mention the way the harness reads it: bare
// where the harness would read the path back exactly, else inside the first
// of its quotes that can hold the path. A path no form can hold is left out:
// offered, it would name another file or none.
func mentionForm(items []harness.Completion, m *spec.Mentions) []harness.Completion {
	if m == nil {
		return items
	}
	out := items[:0]
	for _, it := range items {
		path := strings.TrimSuffix(strings.TrimPrefix(it.Insert, "@"), " ")
		form, ok := mentionOf(path, m)
		if !ok {
			continue
		}
		it.Insert = "@" + form + " "
		out = append(out, it)
	}
	return out
}

func mentionOf(path string, m *spec.Mentions) (string, bool) {
	if path == "" || strings.ContainsAny(path, "\n\r") {
		return "", false
	}
	first, _ := utf8.DecodeRuneInString(path)
	last, _ := utf8.DecodeLastRuneInString(path)
	bare := !strings.ContainsFunc(path, unicode.IsSpace) && !strings.ContainsAny(path, m.BareStops) &&
		!strings.ContainsRune(m.BareLeading, first) && !strings.ContainsRune(m.BareTrailing, last)
	if bare {
		return path, true
	}
	if path != strings.TrimSpace(path) {
		return "", false // quotes drop spaces at either end
	}
	for _, q := range m.Quotes {
		if q != "" && !strings.Contains(path, q) {
			return q + path + q, true
		}
	}
	return "", false
}

// rootsIncompleteText is the refusal when a session's additional workspace
// folders could not all be listed.
const rootsIncompleteText = "This session has workspace folders that could not all be listed. Type the file's path, or open the terminal."
