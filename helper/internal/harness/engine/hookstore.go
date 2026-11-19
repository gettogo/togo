package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// An agent fed only by hooks (Copilot CLI, Droid, Cursor CLI, Amp) has its
// conversation nowhere else Togo can read: the hooks are the chat. The
// engine keeps each such session's hook records, as mapped (redacted) and
// with the mapper's marks, in $TOGO_HOME/run/hooks-<session>.jsonl, and
// replays them through a fresh mapper when the session is served again
// after a helper restart. Replaying the records, not storing the items,
// keeps the mapper the one source of truth: its turn, tools, subagents and
// questions come back as they were, and a later mapper fix applies to old
// records too.
//
// The file is one header line, then one record per line, appended as each
// is mapped (no fsync: a helper restart loses nothing the kernel holds; a
// machine crash can lose the last few). Bounds: hookKeep records and
// hookKeepBytes; past either, the oldest quarter is cut by an atomic
// rewrite. A file not written for hookKeepAge is deleted. An unreadable
// header, a newer version or an oversized file is ignored and replaced; a
// line that does not parse (a torn last line) is skipped and cut. The
// file is never opened through a symbolic link.
//
// A cut can fall inside a turn: the replay then starts mid-turn. The
// hook mappers draw nothing for a tool end whose start was cut, and a
// tool start with no prompt before it opens a turn without its prompt, so
// the oldest turn kept may show without its prompt. The store cannot tell
// where a turn begins without the mapper, and the cut is rare (2000
// records), so it is left so.
//
// Set and cleared: written on each hook record of a hook-only session;
// read once when the session's hook feed first starts in this process;
// deleted when the session is removed (Forget), when the chat starts over
// on another conversation (the store restarts with the announcing
// record), and by age. A helper restart (Unload, or the process ending)
// keeps it. Exits, relaunches, turn ends, interrupts and reconnects are
// records like any other: the chat shows them, so the store keeps them.
const (
	hookStoreVersion = 1
	hookKeep         = 2000
	hookKeepBytes    = 8 << 20
	hookKeepAge      = 7 * 24 * time.Hour
	hookFilePrefix   = "hooks-"
	hookFileSuffix   = ".jsonl"
)

type hookHeader struct {
	V int `json:"togo_hooks"`
}

// hookLine is one stored record.
type hookLine struct {
	Feed string          `json:"feed"`
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
	Pre  json.RawMessage `json:"pre,omitempty"`
	Post json.RawMessage `json:"post,omitempty"`
}

// hookStore is one session's file. Its methods run under the session's
// lock.
type hookStore struct {
	path  string
	n     int
	bytes int64
	gone  bool // removed with its session: nothing is written again
}

// hookStorePath is the session's file, "" when there is no run folder or
// the id is not a plain name.
func hookStorePath(togoHome, id string) string {
	if togoHome == "" || id == "" || id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return ""
	}
	return filepath.Join(togoHome, "run", hookFilePrefix+id+hookFileSuffix)
}

// load reads the stored records, newest last, and leaves the file ready
// for appends: an unusable file is removed, a torn last line cut.
func (h *hookStore) load(now time.Time) []hookLine {
	h.n, h.bytes = 0, 0
	// Never through a link: the file is ours, in our own folder.
	f, err := os.OpenFile(h.path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if !os.IsNotExist(err) {
			_ = os.Remove(h.path)
		}
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 2*hookKeepBytes || now.Sub(st.ModTime()) > hookKeepAge {
		_ = os.Remove(h.path)
		return nil
	}
	r := bufio.NewReader(f)
	head, err := r.ReadBytes('\n')
	var hd hookHeader
	if err != nil || json.Unmarshal(head, &hd) != nil || hd.V != hookStoreVersion {
		// Another version's file, or not ours: start again.
		_ = os.Remove(h.path)
		return nil
	}
	var lines []hookLine
	good := int64(len(head))
	for {
		raw, err := r.ReadBytes('\n')
		if err != nil {
			break // a last line without its newline was cut off mid-write
		}
		good += int64(len(raw))
		var l hookLine
		if json.Unmarshal(raw, &l) != nil || len(l.Data) == 0 {
			continue
		}
		lines = append(lines, l)
		h.bytes += int64(len(raw))
	}
	if good < st.Size() {
		if w, err := os.OpenFile(h.path, os.O_WRONLY|syscall.O_NOFOLLOW, 0); err == nil {
			_ = w.Truncate(good)
			_ = w.Close()
		}
	}
	h.n = len(lines)
	if h.n > hookKeep || h.bytes > hookKeepBytes {
		lines = h.compact(lines)
	}
	return lines
}

// add appends one record, cutting the oldest when a bound is passed.
func (h *hookStore) add(l hookLine) error {
	if h.gone || h.path == "" {
		return nil
	}
	raw, err := json.Marshal(l)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if int64(len(raw)) > hookKeepBytes/4 {
		return errors.New("record too large to keep")
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if st, err := f.Stat(); err == nil && st.Size() == 0 {
		head, _ := json.Marshal(hookHeader{V: hookStoreVersion})
		raw = append(append(head, '\n'), raw...)
		h.n, h.bytes = 0, 0
	}
	_, err = f.Write(raw)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	h.n++
	h.bytes += int64(len(raw))
	if h.n > hookKeep || h.bytes > hookKeepBytes {
		h.load(time.Now()) // reads and cuts
	}
	return nil
}

// compact rewrites the file with the newest records that fit three
// quarters of each bound, and returns them.
func (h *hookStore) compact(lines []hookLine) []hookLine {
	keep, size := 0, int64(0)
	var enc [][]byte
	for i := len(lines) - 1; i >= 0 && keep < hookKeep*3/4; i-- {
		raw, err := json.Marshal(lines[i])
		if err != nil {
			continue
		}
		if size+int64(len(raw))+1 > hookKeepBytes*3/4 {
			break
		}
		enc = append(enc, raw)
		size += int64(len(raw)) + 1
		keep++
	}
	var buf bytes.Buffer
	head, _ := json.Marshal(hookHeader{V: hookStoreVersion})
	buf.Write(head)
	buf.WriteByte('\n')
	for i := len(enc) - 1; i >= 0; i-- {
		buf.Write(enc[i])
		buf.WriteByte('\n')
	}
	if err := writeAtomic(h.path, buf.Bytes(), 0o600); err != nil {
		_ = os.Remove(h.path)
		h.n, h.bytes = 0, 0
		return nil
	}
	h.n, h.bytes = keep, size
	return lines[len(lines)-keep:]
}

// remove deletes the file. With gone, nothing is written to it again.
func (h *hookStore) remove(gone bool) {
	h.gone = h.gone || gone
	h.n, h.bytes = 0, 0
	if h.path != "" {
		_ = os.Remove(h.path)
	}
}

// sweepHookStores deletes the files of sessions not written for
// hookKeepAge: sessions removed while the helper was not running.
func sweepHookStores(togoHome string, now time.Time) {
	if togoHome == "" {
		return
	}
	dir := filepath.Join(togoHome, "run")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, hookFilePrefix) || !strings.HasSuffix(name, hookFileSuffix) {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > hookKeepAge {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// replayRecord is the stored records of one feed as one replay.
func replayRecord(feed string, lines []hookLine) (harness.Record, bool) {
	rec := harness.Record{Source: feed}
	for _, l := range lines {
		if l.Feed != feed {
			continue
		}
		rec.Replay = append(rec.Replay, l.Data)
		rec.ReplayAt = append(rec.ReplayAt, l.At)
		rec.ReplayMarks = append(rec.ReplayMarks, harness.ReplayMark{Pre: l.Pre, Post: l.Post})
		rec.At = l.At
	}
	return rec, len(rec.Replay) > 0
}
