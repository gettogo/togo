package tap

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
)

// offsetSize is the counter file: an 8-byte epoch then the byte total as a
// big-endian uint64.
const offsetSize = 16

// counterEvery bounds how often the counter file is rewritten.
const counterEvery = 100 * time.Millisecond

// OffsetPath is where the tap of a session keeps its byte counter:
// $TOGO_HOME/run/tap/<id>.off.
func OffsetPath(home, id string) (string, error) {
	if _, err := session.ValidateID(id); err != nil {
		return "", err
	}
	return filepath.Join(home, "run", "tap", id+".off"), nil
}

// Counter counts the output bytes a tap has seen and publishes the total,
// with an epoch that is new each time the tap starts, so anchors can say
// where in the pane's output something happened.
type Counter struct {
	path  string
	epoch [8]byte
	now   func() time.Time

	mu      sync.Mutex
	total   int64
	written time.Time
	dirty   bool
}

// NewCounter starts a counter for the session with a fresh epoch and writes
// its first total (zero).
func NewCounter(home, id string) (*Counter, error) {
	p, err := OffsetPath(home, id)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	c := &Counter{path: p, now: time.Now}
	_, _ = rand.Read(c.epoch[:])
	return c, c.Flush()
}

// Epoch is the counter's epoch as hex.
func (c *Counter) Epoch() string { return hex.EncodeToString(c.epoch[:]) }

// Add counts n more bytes, rewriting the file at most every 100 ms.
func (c *Counter) Add(n int) {
	c.mu.Lock()
	c.total += int64(n)
	c.dirty = true
	due := c.now().Sub(c.written) >= counterEvery
	c.mu.Unlock()
	if due {
		_ = c.Flush()
	}
}

// Flush writes the current total now, atomically.
func (c *Counter) Flush() error {
	c.mu.Lock()
	var b [offsetSize]byte
	copy(b[:8], c.epoch[:])
	binary.BigEndian.PutUint64(b[8:], uint64(c.total))
	c.written, c.dirty = c.now(), false
	c.mu.Unlock()
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b[:], 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Close writes the last total if it changed since the last write.
func (c *Counter) Close() error {
	c.mu.Lock()
	dirty := c.dirty
	c.mu.Unlock()
	if dirty {
		return c.Flush()
	}
	return nil
}

// ReadOffset reads a session's counter file: the tap's epoch and the bytes
// it has seen.
func ReadOffset(home, id string) (epoch string, total int64, err error) {
	p, err := OffsetPath(home, id)
	if err != nil {
		return "", 0, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", 0, err
	}
	if len(b) != offsetSize {
		return "", 0, errors.New("tap: counter file has the wrong size")
	}
	n := binary.BigEndian.Uint64(b[8:])
	if n > 1<<62 {
		return "", 0, fmt.Errorf("tap: counter out of range")
	}
	return hex.EncodeToString(b[:8]), int64(n), nil
}
