package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	manifestVersion = 1
	// maxManifestBytes bounds what is read back: 99 sessions at the argv
	// limits stay well under it.
	maxManifestBytes = 4 << 20
	maxCwdBytes      = 4096
	manifestLockWait = 10 * time.Second
)

// ManifestEntry is what the helper keeps about a managed session so it can
// start it again after the tmux server is gone. EndedAt is when the helper
// first found the session gone, zero while it runs.
type ManifestEntry struct {
	ID        string    `json:"id"`
	Bib       string    `json:"bib"`
	Name      string    `json:"name"`
	Agent     string    `json:"agent"`
	Cwd       string    `json:"cwd"`
	Command   []string  `json:"command"`
	CreatedAt time.Time `json:"createdAt"`
	EndedAt   time.Time `json:"endedAt,omitzero"`
	// Muted keeps the session's push mute across a restore.
	Muted bool `json:"muted,omitempty"`
}

// Manifest is the record of managed sessions at Path, normally
// $TOGO_HOME/sessions.json. The helper writes it 0600 through a temporary
// file and a rename. Anyone with the user's rights can edit it, so every
// entry is checked again when read and one that fails is skipped.
type Manifest struct {
	Path string
	// Logf receives why an entry was skipped, never its content. Nil
	// discards it.
	Logf func(format string, args ...any)

	mu sync.Mutex
}

type manifestFile struct {
	V        int             `json:"v"`
	Sessions []ManifestEntry `json:"sessions"`
}

// Put adds e, replacing any entry with its id.
func (m *Manifest) Put(e ManifestEntry) error {
	if err := validEntry(e); err != nil {
		return fmt.Errorf("session record %s: %w", e.ID, err)
	}
	return m.update(func(list []ManifestEntry) []ManifestEntry {
		return append(without(list, e.ID), e)
	})
}

// Remove deletes the entry with id. A missing entry is not an error.
func (m *Manifest) Remove(id string) error {
	return m.update(func(list []ManifestEntry) []ManifestEntry { return without(list, id) })
}

// All returns the valid entries.
func (m *Manifest) All() ([]ManifestEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.read()
}

// update rewrites the file with fn's result, holding the in-process mutex
// and a lock file so the daemon and a CLI process never lose each other's
// writes.
func (m *Manifest) update(fn func([]ManifestEntry) []ManifestEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), manifestLockWait)
	defer cancel()
	return withLock(ctx, m.Path+".lock", func() error {
		list, err := m.read()
		if err != nil {
			return err
		}
		b, err := json.Marshal(manifestFile{V: manifestVersion, Sessions: fn(list)})
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(m.Path), 0o700); err != nil {
			return fmt.Errorf("session records: %w", err)
		}
		return state.WriteFile(m.Path, b)
	})
}

// read loads the file. A missing file is empty; an unreadable one, or one
// of another version, is logged and treated as empty so the next write
// replaces it.
func (m *Manifest) read() ([]ManifestEntry, error) {
	f, err := os.Open(m.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("session records: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("session records: %w", err)
	}
	var file manifestFile
	switch {
	case len(b) > maxManifestBytes:
		m.logf("session records: file is over %d bytes, ignoring it", maxManifestBytes)
		return nil, nil
	case json.Unmarshal(b, &file) != nil:
		m.logf("session records: file is not valid JSON, ignoring it")
		return nil, nil
	case file.V != manifestVersion:
		m.logf("session records: version %d is not %d, ignoring the file", file.V, manifestVersion)
		return nil, nil
	}
	out := make([]ManifestEntry, 0, len(file.Sessions))
	for i, e := range file.Sessions {
		if err := validEntry(e); err != nil {
			m.logf("session records: skipping entry %d: %v", i+1, err)
			continue
		}
		out = append(without(out, e.ID), e)
	}
	return out, nil
}

func (m *Manifest) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}

func without(list []ManifestEntry, id string) []ManifestEntry {
	out := make([]ManifestEntry, 0, len(list))
	for _, e := range list {
		if e.ID != id {
			out = append(out, e)
		}
	}
	return out
}

// validEntry checks e against what Create could have written. Errors name
// the field, never its value.
func validEntry(e ManifestEntry) error {
	if !managedIDPattern.MatchString(e.ID) {
		return errors.New("invalid id")
	}
	if !validBib(e.Bib) {
		return errors.New("invalid number")
	}
	if name, err := normalizeName(e.Name); err != nil || name == "" || name != e.Name {
		return errors.New("invalid name")
	}
	if !validCwd(e.Cwd) {
		return errors.New("folder is not a clean absolute path")
	}
	if e.CreatedAt.IsZero() {
		return errors.New("no creation time")
	}
	return validCommand(e.Agent, e.Command)
}

// validCommand checks that argv is one the agent's sessions run: nil for
// the shell, the registry program for a known agent, any valid program for
// a custom one.
func validCommand(agentID string, argv []string) error {
	if agentID != wire.AgentCustom {
		a, ok := agents.Lookup(agentID)
		switch {
		case !ok:
			return errors.New("unknown agent")
		case a.Command == "" && argv != nil:
			return errors.New("the shell has no command")
		case a.Command == "":
			return nil
		case len(argv) == 0 || argv[0] != a.Command:
			return errors.New("command does not match the agent")
		}
	}
	raw, err := encodeArgv(argv)
	if err != nil {
		return errors.New("invalid command")
	}
	if _, err := decodeArgv(raw); err != nil || argv == nil {
		return errors.New("invalid command")
	}
	return nil
}

// validCwd accepts an absolute path with no "." or ".." elements, valid
// UTF-8 and no control characters. Whether it still exists is checked
// when the session is started again.
func validCwd(p string) bool {
	return p != "" && len(p) <= maxCwdBytes && filepath.IsAbs(p) && filepath.Clean(p) == p &&
		utf8.ValidString(p) && !strings.ContainsFunc(p, unicode.IsControl)
}

// remember records a session Create or Relaunch started. Failing to record
// it does not undo the session; it is logged.
func (s *Service) remember(e ManifestEntry) {
	if s.Manifest == nil {
		return
	}
	if err := s.Manifest.Put(e); err != nil {
		s.logf("%v", err)
	}
}

// rememberName updates a recorded session's name, or records the session
// when it predates the record.
func (s *Service) rememberName(sess Session) {
	if s.Manifest == nil {
		return
	}
	var updated bool
	err := s.Manifest.update(func(list []ManifestEntry) []ManifestEntry {
		for i := range list {
			if list[i].ID == sess.ID {
				list[i].Name, updated = sess.Name, true
			}
		}
		return list
	})
	if err != nil {
		s.logf("session record %s: %v", sess.ID, err)
	} else if !updated {
		s.remember(entryFor(sess))
	}
}

// forget drops a session the user ended.
func (s *Service) forget(id string) {
	if s.Manifest == nil {
		return
	}
	if err := s.Manifest.Remove(id); err != nil {
		s.logf("session record %s: %v", id, err)
	}
}

func entryFor(sess Session) ManifestEntry {
	e := ManifestEntry{ID: sess.ID, Name: sess.Name, Agent: sess.Agent, Cwd: sess.Cwd, Command: sess.Command, CreatedAt: sess.CreatedAt, Muted: sess.Muted}
	if sess.Bib != nil {
		e.Bib = *sess.Bib
	}
	return e
}

func (s *Service) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}
