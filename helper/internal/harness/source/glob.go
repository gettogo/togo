package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// GlobEntry is one file a glob source lists.
type GlobEntry struct {
	Path  string            `json:"path"`
	Name  string            `json:"name"`
	Mtime time.Time         `json:"mtime"`
	Head  []json.RawMessage `json:"head"`
	Tail  []json.RawMessage `json:"tail"`
}

// Glob lists the confined files matching pattern, newest first, at most
// MaxGlobFiles. For jsonl files Head and Tail hold the first and last
// records; a json file's document is Head's only element. No match is an
// empty list: a harness with no sessions yet has nothing to list.
func Glob(pattern, format string, env harness.Env) ([]GlobEntry, error) {
	matches, err := globConfined(pattern, env)
	if err != nil {
		return nil, err
	}
	out := make([]GlobEntry, 0, len(matches))
	for _, m := range matches {
		e := GlobEntry{Path: m.path, Mtime: m.mtime.UTC(), Name: strings.TrimSuffix(filepath.Base(m.path), filepath.Ext(m.path))}
		e.Head, e.Tail = headTail(m.path, format)
		out = append(out, e)
	}
	return out, nil
}

type match struct {
	path  string
	mtime time.Time
	size  int64
	born  time.Time // creation time, zero when unknown
}

// globConfined expands, matches and confines a glob, newest first.
func globConfined(pattern string, env harness.Env) ([]match, error) {
	pattern = Expand(pattern, home(env))
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(Expand(env.Cwd, home(env)), pattern)
	}
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	var out []match
	for _, p := range paths {
		real, err := Confine(p, env)
		if err != nil {
			continue
		}
		st, err := os.Stat(real)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		out = append(out, match{path: real, mtime: st.ModTime(), size: st.Size(), born: birth(real, st)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].mtime.After(out[j].mtime) })
	if len(out) > MaxGlobFiles {
		out = out[:MaxGlobFiles]
	}
	return out, nil
}

func headTail(p, format string) (head, tail []json.RawMessage) {
	head, tail = []json.RawMessage{}, []json.RawMessage{}
	fh, err := openRegular(p)
	if err != nil {
		return
	}
	defer fh.Close()
	if format != "jsonl" {
		b, err := io.ReadAll(io.LimitReader(fh, MaxFileRead+1))
		if err == nil && len(b) <= MaxFileRead {
			if raw, err := Convert(b, format); err == nil {
				head = append(head, raw)
			}
		}
		return
	}
	first := make([]byte, 64<<10)
	n, _ := io.ReadFull(fh, first)
	recs := Records(dropPartialLast(first[:n], n == len(first)), 1<<30)
	if len(recs) > globHeadRecords {
		recs = recs[:globHeadRecords]
	}
	head = recs
	st, err := fh.Stat()
	if err != nil {
		return
	}
	off := max(st.Size()-globTailBytes, 0)
	buf := make([]byte, st.Size()-off)
	if _, err := fh.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return
	}
	if off > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	tail = Records(buf, globTailRecords)
	return
}

func dropPartialLast(b []byte, full bool) []byte {
	if !full {
		return b
	}
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return b[:i]
	}
	return nil
}
