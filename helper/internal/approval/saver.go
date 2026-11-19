package approval

import "sync"

// saver writes the open approvals from one goroutine, keeping only the
// latest version, so no caller waits for the disk while holding the
// store's lock: a slow or full disk delays the file, never an answer.
type saver struct {
	logf  func(string, ...any)
	write func(path string, b []byte) error

	mu      sync.Mutex
	idle    *sync.Cond
	path    string
	latest  []byte
	waiting bool // latest is not written yet
	busy    bool // the writing goroutine runs
}

func newSaver(logf func(string, ...any)) *saver {
	v := &saver{logf: logf, write: writeKept}
	v.idle = sync.NewCond(&v.mu)
	return v
}

// put replaces what is to be written; b nil removes the file.
func (v *saver) put(path string, b []byte) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.path, v.latest, v.waiting = path, b, true
	if !v.busy {
		v.busy = true
		go v.run()
	}
}

func (v *saver) run() {
	v.mu.Lock()
	for v.waiting {
		path, b, write := v.path, v.latest, v.write
		v.waiting = false
		v.mu.Unlock()
		if err := write(path, b); err != nil {
			v.logf("approval: the open requests could not be kept for a restart: %v", err)
		}
		v.mu.Lock()
	}
	v.busy = false
	v.idle.Broadcast()
	v.mu.Unlock()
}

// flush waits until the latest version is written.
func (v *saver) flush() {
	if v == nil {
		return
	}
	v.mu.Lock()
	for v.busy || v.waiting {
		v.idle.Wait()
	}
	v.mu.Unlock()
}

// Flush waits until the open approvals are on disk, for a clean shutdown.
func (s *Store) Flush() { s.saver.flush() }
