package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Polling and listing tunables.
var (
	PollEvery = 500 * time.Millisecond
	// globEvery is how often a followed glob looks for new files.
	globEvery = 2 * time.Second
)

const (
	globHeadRecords = 8
	globTailBytes   = 256 << 10
	globTailRecords = 200
)

// File reads file and jsonl sources and tails them as feeds.
type File struct{ kind string }

func (f File) Kind() string { return f.kind }

func decodeFile(s harness.SourceSpec) (spec.File, error) {
	var fs spec.File
	if err := json.Unmarshal(s.Raw, &fs); err != nil {
		return fs, fmt.Errorf("source %s: %w", s.Name, err)
	}
	if (fs.Path == "") == (fs.Glob == "") {
		return fs, fmt.Errorf("source %s: set exactly one of path and glob", s.Name)
	}
	return fs, nil
}

func (f File) format(fs spec.File) string {
	if fs.Format != "" {
		return fs.Format
	}
	if f.kind == spec.KindJSONL {
		return "jsonl"
	}
	return "json"
}

// Fetch reads the file (capped), or lists a glob's matches newest first.
func (f File) Fetch(_ context.Context, s harness.SourceSpec, env harness.Env) (json.RawMessage, error) {
	fs, err := decodeFile(s)
	if err != nil {
		return nil, err
	}
	if fs.Glob != "" {
		list, err := Glob(fs.Glob, f.format(fs), env)
		if err != nil {
			return nil, err
		}
		return json.Marshal(list)
	}
	p, err := Confine(fs.Path, env)
	if errors.Is(err, os.ErrNotExist) {
		// Not written yet (a todo file before the first todo): empty.
		return json.RawMessage("null"), nil
	}
	if err != nil {
		return nil, err
	}
	b, err := readCapped(p)
	if errors.Is(err, os.ErrNotExist) {
		return json.RawMessage("null"), nil
	}
	if err != nil {
		return nil, err
	}
	return Convert(b, f.format(fs))
}

func readCapped(p string) ([]byte, error) {
	fh, err := openRegular(p)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	b, err := io.ReadAll(io.LimitReader(fh, MaxFileRead+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFileRead {
		return nil, ErrTooLarge
	}
	return b, nil
}

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
	recs := Records(dropPartialLast(first[:n], n == len(first)), globHeadRecords)
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

// Watch tails a jsonl source (or re-reads a json file on change) until
// ctx ends.
func (f File) Watch(ctx context.Context, s harness.SourceSpec, env harness.Env, emit func(harness.Record)) error {
	fs, err := decodeFile(s)
	if err != nil {
		return err
	}
	lines := f.kind == spec.KindJSONL && fs.Format == FormatLines
	if f.format(fs) != "jsonl" && !lines {
		return watchWhole(ctx, s.Name, fs, f.format(fs), env, emit)
	}
	if fs.Path != "" {
		p, err := Confine(fs.Path, env)
		if err != nil {
			return err
		}
		t := &tail{path: p, name: s.Name, replay: true, lines: lines}
		return pollLoop(ctx, func() error { return t.poll(emit) })
	}
	if fs.Exclusive {
		return watchGlobExclusive(ctx, s.Name, fs.Glob, env, emit)
	}
	return watchGlob(ctx, s.Name, fs.Glob, env, emit)
}

func pollLoop(ctx context.Context, step func() error) error {
	tick := time.NewTicker(PollEvery)
	defer tick.Stop()
	for {
		if err := step(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// watchGlob tails the newest match from its start, then any match that
// changes after the watch began, from its start when it is new. When the
// session's creation time is known, the newest match is taken at once only
// if it was written since then: an older file is an earlier session's in the
// same folder, joined only if it changes (a resume writing to it).
func watchGlob(ctx context.Context, name, pattern string, env harness.Env, emit func(harness.Record)) error {
	started := time.Now()
	created, err := time.Parse(time.RFC3339, env.Vars["created"])
	if err != nil {
		created = time.Time{}
	}
	tails := map[string]*tail{}
	var lastGlob time.Time
	every := globEvery
	return pollLoop(ctx, func() error {
		if time.Since(lastGlob) >= every || len(tails) == 0 {
			lastGlob = time.Now()
			ms, err := globConfined(pattern, env)
			if err != nil {
				return err
			}
			for i, m := range ms {
				if _, ok := tails[m.path]; ok {
					continue
				}
				if (i == 0 && len(tails) == 0 && !m.mtime.Before(created)) || m.mtime.After(started) {
					tails[m.path] = &tail{path: m.path, name: name, replay: true}
				}
			}
		}
		for _, t := range tails {
			if err := t.poll(emit); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return nil
	})
}

// FormatLines tails a text file (a followed jsonl block with a path):
// each poll emits one record, a JSON array of the whole lines it read,
// blank lines kept and a trailing CR dropped. Aider's markdown chat
// history is read this way.
const FormatLines = "lines"

// tail follows one jsonl file by offset.
type tail struct {
	lines   bool
	path    string
	name    string
	offset  int64
	pending []byte
	mtime   time.Time
	size    int64
	replay  bool
	skip    bool
}

func (t *tail) poll(emit func(harness.Record)) error {
	st, err := os.Stat(t.path)
	if err != nil {
		return err
	}
	if st.Size() == t.size && st.ModTime().Equal(t.mtime) {
		return nil
	}
	if st.Size() < t.offset {
		// Truncated or replaced: start over.
		t.offset, t.pending = 0, nil
		t.replay = true
	}
	t.size, t.mtime = st.Size(), st.ModTime()
	fh, err := openRegular(t.path)
	if err != nil {
		return err
	}
	defer fh.Close()
	var recs []json.RawMessage
	replaying := t.replay
	if t.replay {
		t.replay = false
		// Replay at most the last MaxFileRead bytes, then the last
		// MaxRecords records of those.
		if start := st.Size() - MaxFileRead; start > t.offset {
			t.offset, t.skip = start, true
		}
	}
	for t.offset < st.Size() {
		n := min(st.Size()-t.offset, MaxFileRead)
		buf := make([]byte, n)
		got, err := fh.ReadAt(buf, t.offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if got == 0 {
			break
		}
		buf = buf[:got]
		t.offset += int64(got)
		data := append(t.pending, buf...)
		if t.skip {
			// Replay began mid-line: drop the partial first line.
			j := bytes.IndexByte(data, '\n')
			if j < 0 {
				t.pending = nil
				continue
			}
			data, t.skip = data[j+1:], false
		}
		i := bytes.LastIndexByte(data, '\n')
		if i < 0 {
			t.pending = data
			if len(t.pending) > MaxFileRead {
				t.pending = nil // a line too long to keep
			}
			continue
		}
		if t.lines {
			recs = append(recs, lineRecord(data[:i]))
		} else {
			recs = append(recs, Records(data[:i], 1<<30)...)
		}
		t.pending = append([]byte(nil), data[i+1:]...)
	}
	if len(recs) > MaxRecords && !t.lines {
		recs = recs[len(recs)-MaxRecords:]
	}
	now := time.Now()
	if replaying && len(recs) > 0 {
		emit(harness.Record{Source: t.name, At: now, Replay: recs})
		return nil
	}
	for _, r := range recs {
		emit(harness.Record{Source: t.name, At: now, Data: r})
	}
	return nil
}

// lineRecord is one lines-format record: b split on newlines.
func lineRecord(b []byte) json.RawMessage {
	parts := bytes.Split(b, []byte("\n"))
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = string(bytes.TrimSuffix(p, []byte("\r")))
	}
	raw, _ := json.Marshal(out)
	return raw
}

// watchWhole emits a json or toml file's document each time it changes.
func watchWhole(ctx context.Context, name string, fs spec.File, format string, env harness.Env, emit func(harness.Record)) error {
	var last time.Time
	var lastSize int64 = -1
	return pollLoop(ctx, func() error {
		var p string
		if fs.Glob != "" {
			ms, err := globConfined(fs.Glob, env)
			if err != nil || len(ms) == 0 {
				return nil
			}
			p = ms[0].path
		} else {
			var err error
			if p, err = Confine(fs.Path, env); err != nil {
				return err
			}
		}
		st, err := os.Stat(p)
		if err != nil {
			return nil
		}
		if st.ModTime().Equal(last) && st.Size() == lastSize {
			return nil
		}
		last, lastSize = st.ModTime(), st.Size()
		b, err := readCapped(p)
		if err != nil {
			return nil
		}
		raw, err := Convert(b, format)
		if err != nil {
			return nil
		}
		emit(harness.Record{Source: name, At: time.Now(), Data: raw})
		return nil
	})
}

// Sole returns the one file matching pattern written at or after since.
// When several were, it cannot tell which belongs to the caller and
// returns ErrAmbiguous: another session in the same folder writes there
// too, and guessing would show it its neighbour's conversation.
func Sole(pattern string, since time.Time, env harness.Env) (string, error) {
	ms, err := globConfined(pattern, env)
	if err != nil {
		return "", err
	}
	var fresh []match
	for _, m := range ms {
		if !m.mtime.Before(since) {
			fresh = append(fresh, m)
		}
	}
	switch len(fresh) {
	case 0:
		return "", os.ErrNotExist
	case 1:
		return fresh[0].path, nil
	}
	return "", ErrAmbiguous
}

// ErrAmbiguous is Sole's answer when more than one file qualifies.
var ErrAmbiguous = errors.New("source: more than one file matches")

// errNotRegular refuses a source path that is not a regular file: a FIFO
// or device would block a read past the source's timeout.
var errNotRegular = errors.New("source: not a regular file")

// openRegular opens p for reading without blocking on a FIFO and refuses
// anything but a regular file.
func openRegular(p string) (*os.File, error) {
	fh, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	st, err := fh.Stat()
	if err != nil || !st.Mode().IsRegular() {
		fh.Close()
		return nil, errNotRegular
	}
	return fh, nil
}
