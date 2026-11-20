package engine

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/gettogo/togo/helper/internal/session"
)

// fenceSeq numbers fence writes, taken under the session's lock.
var fenceSeq atomic.Uint64

// fenceSaves orders the writes of each session's fence: a write is
// skipped when a later one already landed.
var fenceSaves struct {
	sync.Mutex
	seq map[string]uint64
}

// maxFenceFile caps what is read of a kept fence: 1000 ids with their
// keys stay well under it.
const maxFenceFile = 256 << 10

func (e *Engine) exitFencePath(id string) string {
	if e.cfg.TogoHome == "" {
		return ""
	}
	if _, err := session.ValidateID(id); err != nil {
		return ""
	}
	return filepath.Join(e.cfg.TogoHome, "run", "harness", id+".exit")
}

// saveExitFence writes the fence away from the session's lock.
func (e *Engine) saveExitFence(id string, seq uint64, b []byte) {
	path := e.exitFencePath(id)
	if path == "" {
		return
	}
	fenceSaves.Lock()
	defer fenceSaves.Unlock()
	if fenceSaves.seq == nil {
		fenceSaves.seq = map[string]uint64{}
	}
	if fenceSaves.seq[path] >= seq {
		return
	}
	fenceSaves.seq[path] = seq
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		e.fenceFailed(id, path, err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		e.fenceFailed(id, path, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		e.fenceFailed(id, path, err)
	}
}

// fenceFailed logs, once per file, that a fence could not be kept: after
// a helper restart the session may show the dead turn running again.
func (e *Engine) fenceFailed(id, path string, err error) {
	if _, logged := fenceFailures.LoadOrStore(path, true); !logged {
		e.cfg.Logf("harness: %s: the exit could not be kept for a restart: %v", id, err)
	}
}

// fenceFailures are the fence files whose failure was logged.
var fenceFailures sync.Map

// loadExitFence reads a fence a previous run of the helper kept. A file
// over maxFenceFile or one that does not read is dropped and logged.
func (e *Engine) loadExitFence(id string) *exitFence {
	path := e.exitFencePath(id)
	if path == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, maxFenceFile+1))
	if err != nil || len(b) > maxFenceFile {
		e.cfg.Logf("harness: %s: the kept exit is unreadable or too large; it is dropped", id)
		return nil
	}
	var f exitFence
	if json.Unmarshal(b, &f) != nil || len(f.Items)+len(f.Rows) > maxFenceItems || len(f.Masks) > 64 {
		e.cfg.Logf("harness: %s: the kept exit does not read; it is dropped", id)
		return nil
	}
	if f.Items == nil {
		f.Items = map[string]bool{}
	}
	return &f
}

// forgetExitFence removes the kept fence with its session.
func (e *Engine) forgetExitFence(id string) {
	path := e.exitFencePath(id)
	if path == "" {
		return
	}
	fenceSaves.Lock()
	defer fenceSaves.Unlock()
	if fenceSaves.seq == nil {
		fenceSaves.seq = map[string]uint64{}
	}
	// A write still on its way must not bring the file back.
	fenceSaves.seq[path] = fenceSeq.Add(1)
	_ = os.Remove(path)
}
