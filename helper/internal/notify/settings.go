// Package notify keeps this machine's notification settings: how long output
// stays quiet before "done" is sent, whether the machine is muted, and when
// an unanswered approval is reminded.
package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
)

// Settings are the per-machine notification choices.
type Settings struct {
	IdleSeconds   int  `json:"idleSeconds"`
	Muted         bool `json:"muted"`
	RemindMinutes int  `json:"remindMinutes"`
}

// Defaults are used until the user changes anything.
var Defaults = Settings{IdleSeconds: 6, Muted: false, RemindMinutes: 10}

// Ranges.
const (
	MinIdleSeconds   = 3
	MaxIdleSeconds   = 600
	MinRemindMinutes = 2
	MaxRemindMinutes = 120
)

func validIdle(n int) error {
	if n < MinIdleSeconds || n > MaxIdleSeconds {
		return &session.InvalidParamsError{Msg: fmt.Sprintf("idleSeconds must be %d to %d", MinIdleSeconds, MaxIdleSeconds)}
	}
	return nil
}

func validRemind(n int) error {
	if n != 0 && (n < MinRemindMinutes || n > MaxRemindMinutes) {
		return &session.InvalidParamsError{Msg: fmt.Sprintf("remindMinutes must be 0 or %d to %d", MinRemindMinutes, MaxRemindMinutes)}
	}
	return nil
}

// Patch is a partial update; nil fields keep their value.
type Patch struct {
	IdleSeconds   *int
	Muted         *bool
	RemindMinutes *int
}

// Store holds the settings in a file. It is the file's only writer, but
// the file is read again whenever its size or modification time changes,
// so an edit by hand still takes effect.
type Store struct {
	// Path is $TOGO_HOME/notify.json.
	Path string
	// OnChange is called after a successful Set with the new settings.
	OnChange func(Settings)
	// Logf receives one line for an unreadable file. Nil discards.
	Logf func(format string, args ...any)

	mu     sync.Mutex
	logged bool
	// cached is the file's settings as of its stat in cachedInfo; nil
	// cachedInfo means nothing is cached.
	cached     Settings
	cachedInfo os.FileInfo
}

// Get returns the settings. A missing file gives the defaults, and a field
// that is missing, mistyped or out of range gives its default (logged
// once).
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() Settings {
	fi, err := os.Stat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		s.cachedInfo = nil
		return Defaults
	}
	if err == nil && s.cachedInfo != nil && fi.Size() == s.cachedInfo.Size() && fi.ModTime().Equal(s.cachedInfo.ModTime()) {
		return s.cached
	}
	cur, err := s.read()
	if err != nil {
		if !s.logged && s.Logf != nil {
			s.Logf("notify: %s unreadable, using defaults where needed: %v", s.Path, err)
		}
		s.logged = true
	}
	s.cached, s.cachedInfo = cur, fi
	return cur
}

// read parses the file field by field, so one bad value does not discard
// the others: a machine the file says is muted stays muted. err reports
// the first problem.
func (s *Store) read() (Settings, error) {
	cur := Defaults
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return cur, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return cur, err
	}
	var first error
	keep := func(err error) {
		if first == nil {
			first = err
		}
	}
	if raw, ok := fields["idleSeconds"]; ok {
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			keep(err)
		} else if err := validIdle(n); err != nil {
			keep(err)
		} else {
			cur.IdleSeconds = n
		}
	}
	if raw, ok := fields["muted"]; ok {
		if err := json.Unmarshal(raw, &cur.Muted); err != nil {
			cur.Muted = Defaults.Muted
			keep(err)
		}
	}
	if raw, ok := fields["remindMinutes"]; ok {
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			keep(err)
		} else if err := validRemind(n); err != nil {
			keep(err)
		} else {
			cur.RemindMinutes = n
		}
	}
	return cur, first
}

// Set applies p and returns the new settings. A value out of range is a
// *session.InvalidParamsError and nothing changes.
func (s *Store) Set(p Patch) (Settings, error) {
	s.mu.Lock()
	cur := s.load()
	if p.IdleSeconds != nil {
		if err := validIdle(*p.IdleSeconds); err != nil {
			s.mu.Unlock()
			return cur, err
		}
		cur.IdleSeconds = *p.IdleSeconds
	}
	if p.RemindMinutes != nil {
		if err := validRemind(*p.RemindMinutes); err != nil {
			s.mu.Unlock()
			return cur, err
		}
		cur.RemindMinutes = *p.RemindMinutes
	}
	if p.Muted != nil {
		cur.Muted = *p.Muted
	}
	b, err := json.Marshal(cur)
	if err == nil {
		if err = os.MkdirAll(filepath.Dir(s.Path), 0o700); err == nil {
			err = state.WriteFile(s.Path, append(b, '\n'))
		}
	}
	if err == nil {
		if fi, serr := os.Stat(s.Path); serr == nil {
			s.cached, s.cachedInfo = cur, fi
		} else {
			s.cachedInfo = nil
		}
	}
	s.mu.Unlock()
	if err != nil {
		return cur, err
	}
	if s.OnChange != nil {
		s.OnChange(cur)
	}
	return cur, nil
}
