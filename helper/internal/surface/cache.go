package surface

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/gettogo/togo/helper/internal/state"
)

const maxJournalBytes = 16 << 20
const maxSnapshotBytes = MaxDocumentBytes + 64*1024

// ErrCachePending means the scene cannot be read yet: a received update is
// not durable, or the program has not yet drawn it for this renderer.
var ErrCachePending = errors.New("surface: document update is being committed")

type cacheFailure struct{ cause error }

func (e *cacheFailure) Error() string { return "surface: document cache unavailable" }
func (e *cacheFailure) Unwrap() error { return e.cause }

// CacheIdentity binds stored documents to one original process and conversation.
type CacheIdentity struct {
	Generation   string `json:"generation"`
	PID          int    `json:"pid"`
	Birth        string `json:"birth"`
	Conversation string `json:"conversation"`
	Epoch        uint64 `json:"epoch"`
}

// Cache journals received updates before exposing their new revision. A
// separate operation lock serializes writes without blocking snapshot readers
// on disk I/O. Scene access is protected by mu.
type Cache struct {
	applyMu  sync.Mutex
	mu       sync.Mutex
	scene    *Scene
	identity CacheIdentity
	dir      string
	file     *os.File
	pending  bool
	// stale: the scene was restored from disk and the program has not yet
	// greeted this renderer. What it holds is from an earlier renderer:
	// kept for the program to adopt, never shown or acted on as current.
	stale      bool
	fatal      error
	journalLen int64
	maxJournal int64
	entries    int
}

type cacheSnapshot struct {
	Version  int             `json:"version"`
	Identity CacheIdentity   `json:"identity"`
	Scene    json.RawMessage `json:"scene"`
}

type cacheEntry struct {
	Revision uint64            `json:"revision"`
	Verb     byte              `json:"verb"`
	Params   map[string]string `json:"params,omitempty"`
	Body     json.RawMessage   `json:"body"`
}

// OpenCache replays only a matching runtime's complete durable records.
func OpenCache(dir string, identity CacheIdentity, options RenderOptions) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	c := &Cache{scene: NewScene(options), identity: identity, dir: dir, maxJournal: maxJournalBytes}
	path := filepath.Join(dir, "snapshot.json")
	if st, err := os.Stat(path); err == nil {
		if st.Size() > maxSnapshotBytes || !st.Mode().IsRegular() {
			return nil, errors.New("surface: invalid cache snapshot")
		}
		raw, err := os.ReadFile(path)
		var snapshot cacheSnapshot
		if err != nil || json.Unmarshal(raw, &snapshot) != nil || snapshot.Version != 1 || !reflect.DeepEqual(snapshot.Identity, identity) {
			return nil, errors.New("surface: cache identity changed")
		}
		if c.scene, err = RestoreScene(snapshot.Scene, options); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err := c.saveSnapshot(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "frames.jsonl"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	c.file = f
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, err
	}
	if err := c.replay(); err != nil {
		f.Close()
		return nil, err
	}
	c.stale = len(c.scene.Views) > 0
	return c, nil
}

// current reports whether the scene is the program's own picture for this
// renderer: it has greeted the renderer since the scene was restored, and
// every view it took up again has had its first frame. Until then a view
// may still hold what an earlier renderer left (a list since closed), and
// reads and inputs wait. Callers hold mu.
func (c *Cache) current() bool {
	if c.stale {
		return false
	}
	for _, v := range c.scene.Views {
		if !v.Closed && v.Adopting {
			return false
		}
	}
	return true
}

func (c *Cache) replay() error {
	st, err := c.file.Stat()
	if err != nil || st.Size() > maxJournalBytes {
		return errors.New("surface: invalid cache journal size")
	}
	r := bufio.NewReaderSize(io.LimitReader(c.file, maxJournalBytes+1), 32*1024)
	var complete int64
	for {
		line, err := r.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			// A partial final write was never exposed as a durable revision.
			if len(line) != 0 {
				if err := c.file.Truncate(complete); err != nil {
					return err
				}
				if err := c.file.Sync(); err != nil {
					return err
				}
			}
			break
		}
		if err != nil || len(line) > MaxMessageBytes+64*1024 {
			return errors.New("surface: cache journal read failed")
		}
		var record cacheEntry
		if json.Unmarshal(line, &record) != nil || record.Revision == 0 {
			return errors.New("surface: corrupted cache journal")
		}
		complete += int64(len(line))
		if record.Revision <= c.scene.Revision {
			continue // Snapshot replacement may have preceded journal truncation.
		}
		if record.Revision != c.scene.Revision+1 {
			return errors.New("surface: cache journal gap")
		}
		if _, err := c.scene.Handle(Message{Verb: record.Verb, Params: record.Params, Body: record.Body}); err != nil || c.scene.Revision != record.Revision {
			return errors.New("surface: cache journal could not be restored")
		}
		c.entries++
	}
	c.journalLen = complete
	_, err = c.file.Seek(0, io.SeekEnd)
	return err
}

// Apply commits a protocol update before its new scene can be read or ACKed.
func (c *Cache) Apply(m Message) ([]Message, error) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.mu.Lock()
	if c.file == nil || c.fatal != nil {
		c.mu.Unlock()
		return nil, &cacheFailure{cause: c.fatal}
	}
	before := c.scene.Revision
	replies, err := c.scene.Handle(m)
	if m.Verb == 'q' && err == nil && len(replies) == 1 && replies[0].Verb == 'r' {
		c.stale = false // the program greeted this renderer
	}
	if err != nil || c.scene.Revision == before {
		c.mu.Unlock()
		return replies, err
	}
	c.pending = true
	record, err := json.Marshal(cacheEntry{Revision: c.scene.Revision, Verb: m.Verb, Params: m.Params, Body: m.Body})
	c.mu.Unlock()
	if err == nil {
		record = append(record, '\n')
		if c.journalLen+int64(len(record)) > c.maxJournal || c.entries >= 256 {
			err = c.checkpoint()
		} else {
			var n int
			n, err = c.file.Write(record)
			if err == nil && n != len(record) {
				err = io.ErrShortWrite
			}
			if err == nil {
				err = c.file.Sync()
			}
			c.journalLen += int64(n)
			c.entries++
		}
	}
	c.mu.Lock()
	if err != nil {
		err = &cacheFailure{cause: err}
	}
	c.pending, c.fatal = false, err
	c.mu.Unlock()
	return replies, err
}

func (c *Cache) saveSnapshot() error {
	c.mu.Lock()
	scene, err := json.Marshal(c.scene)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(cacheSnapshot{Version: 1, Identity: c.identity, Scene: scene})
	if err != nil {
		return err
	}
	if len(raw) > maxSnapshotBytes {
		return ErrLimit
	}
	return state.WriteFile(filepath.Join(c.dir, "snapshot.json"), raw)
}

func (c *Cache) checkpoint() error {
	if err := c.saveSnapshot(); err != nil {
		return err
	}
	if err := c.file.Truncate(0); err != nil {
		return err
	}
	if err := c.file.Sync(); err != nil {
		return err
	}
	c.journalLen, c.entries = 0, 0
	return nil
}

// Snapshot returns immutable wire bytes of the latest durable scene.
func (c *Cache) Snapshot() ([]byte, uint64, error) {
	raw, revision, _, err := c.snapshot(0)
	return raw, revision, err
}

func (c *Cache) snapshot(after uint64) ([]byte, uint64, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fatal != nil {
		return nil, 0, false, c.fatal
	}
	if c.pending || !c.current() {
		return nil, 0, false, ErrCachePending
	}
	if after != 0 && after == c.scene.Revision {
		return nil, c.scene.Revision, true, nil
	}
	raw, err := json.Marshal(c.scene)
	return raw, c.scene.Revision, false, err
}

// Inspect runs a nonblocking read of durable scene state under its lock.
func (c *Cache) Inspect(fn func(*Scene) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fatal != nil {
		return c.fatal
	}
	if c.pending || !c.current() {
		return ErrCachePending
	}
	return fn(c.scene)
}

// Close joins in-progress cache writes before releasing the journal file.
func (c *Cache) Close() {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file != nil {
		c.file.Close()
		c.file = nil
	}
}
