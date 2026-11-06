package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/wire"
)

// optionExit holds a session's SavedExit, so an ended or failed session
// still reads that way after the daemon restarts.
const optionExit = "@togo_exit"

// SavedExit is an exit as stored in the session's @togo_exit option.
type SavedExit struct {
	Status string           `json:"status"`
	Exit   wire.SessionExit `json:"exit"`
}

// SaveExit stores e in the session's @togo_exit option; nil removes it.
func (s *Service) SaveExit(ctx context.Context, id string, e *SavedExit) error {
	if err := requireManaged(id); err != nil {
		return err
	}
	var err error
	if e == nil {
		err = s.Tmux.UnsetOption(ctx, id, optionExit)
	} else {
		var b []byte
		if b, err = json.Marshal(e); err == nil {
			err = s.Tmux.SetOption(ctx, id, optionExit, string(b))
		}
	}
	if err != nil {
		return notFoundOr(err, id)
	}
	return nil
}

// withExit sets sess.SavedExit from its optionExit value. Anyone who can
// run tmux on the socket can rewrite the option, so a value SaveExit could
// not have written is logged, without its content, and ignored.
func (s *Service) withExit(sess Session, raw string) Session {
	e, err := decodeExit(raw)
	if err != nil && s.Logf != nil {
		s.Logf("session %s: ignoring %s: %v", sess.ID, optionExit, err)
	}
	sess.SavedExit = e
	return sess
}

// decodeExit reads an optionExit value. Unset is nil.
func decodeExit(v string) (*SavedExit, error) {
	if v == "" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(v)))
	dec.DisallowUnknownFields()
	var e SavedExit
	if dec.Decode(&e) != nil || dec.More() {
		return nil, errors.New("not a JSON exit record")
	}
	if e.Status != wire.StatusEnded && e.Status != wire.StatusFailed {
		return nil, errors.New("status is not ended or failed")
	}
	if err := e.Exit.Validate(); err != nil {
		return nil, err
	}
	return &e, nil
}

// AgentGone reports whether the session's pane shows no agent running
// (see the AgentGone function). A missing session is ErrNotFound.
func (s *Service) AgentGone(ctx context.Context, id string) (bool, error) {
	fg, err := s.Tmux.Foreground(ctx, id)
	if err != nil {
		return false, notFoundOr(err, id)
	}
	return AgentGone(fg, s.TogoPath), nil
}

// AgentGone reports whether fg shows no agent running: the pane is dead,
// or every process of its foreground job is the pane's own process (the
// wrapper or the login shell it became) or the helper at togoPath.
func AgentGone(fg tmux.PaneForeground, togoPath string) bool {
	if fg.Dead {
		return true
	}
	if fg.PanePID < 1 {
		return false
	}
	helper := filepath.Base(togoPath)
	for _, p := range fg.Foreground {
		if p.PID == fg.PanePID || isHelper(p.Name, helper) {
			continue
		}
		return false
	}
	return true
}

// isHelper matches a process name against the helper's file name. Linux
// reports at most 15 bytes of the name.
func isHelper(name, helper string) bool {
	if name == "" || helper == "" || helper == "." || helper == "/" {
		return false
	}
	if name == helper {
		return true
	}
	return len(name) == 15 && len(helper) > 15 && helper[:15] == name
}
