// Package session is the helper's domain layer for terminal sessions: it
// creates, lists, renames and kills managed tmux sessions on the togo socket
// and shows the user's own tmux sessions read-only.
package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux"
)

// ErrNotFound is returned when no session has the given id or bib.
var ErrNotFound = errors.New("session not found")

// ErrNotManaged is returned by write calls on an unmanaged session.
var ErrNotManaged = errors.New("session not managed")

const (
	requiredMajor = 3
	requiredMinor = 2

	statusIdle   = "idle"
	statusExited = "exited"
)

// TmuxUnavailableError means tmux is missing or older than 3.2.
type TmuxUnavailableError struct {
	// Found is the reported version, or "" when tmux is not installed.
	Found string
}

// Error names the required version and what was found.
func (e *TmuxUnavailableError) Error() string {
	if e.Found == "" {
		return "tmux 3.2 or newer is required, but tmux is not installed"
	}
	return fmt.Sprintf("tmux 3.2 or newer is required, found %s", e.Found)
}

// InvalidParamsError is a rejected input. Msg is safe to show to the user.
type InvalidParamsError struct {
	Msg string
}

// Error returns Msg.
func (e *InvalidParamsError) Error() string { return e.Msg }

// Session is the protocol v1 Session object.
type Session struct {
	ID              string    `json:"id"`
	Bib             *string   `json:"bib"`
	Name            string    `json:"name"`
	Agent           string    `json:"agent"`
	Cwd             string    `json:"cwd"`
	CreatedAt       time.Time `json:"createdAt"`
	Status          string    `json:"status"`
	Activity        *string   `json:"activity"`
	AttachedClients int       `json:"attachedClients"`
	Managed         bool      `json:"managed"`
}

// Service runs session operations. Construct it with NewService, or set the
// fields directly in tests. A Service must not be copied after first use.
type Service struct {
	// Tmux is the client for the togo socket, where managed sessions live.
	Tmux *tmux.Client
	// User is the client for the user's default socket, listed read-only.
	// Nil disables unmanaged sessions.
	User *tmux.Client
	// Recent records the folders sessions are created in.
	Recent *state.Recent
	// LockFile serializes bib allocation across processes.
	LockFile string
	// Shell runs agent commands; empty means /bin/sh.
	Shell string
	// Path is searched for agent commands; empty means state.LoginPath.
	Path string
	// NewID and Now are test seams; nil means a random id and time.Now.
	NewID func() string
	Now   func() time.Time

	mu          sync.Mutex
	tmuxChecked bool
	tmuxErr     error
}

const tmuxConf = `set -g mouse on
set -g set-clipboard on
set -s escape-time 0
set -g history-limit 50000
set -g default-terminal "tmux-256color"
`

// NewService returns the production Service rooted at home (see
// state.Home). It rewrites home/tmux.conf on every start so the file always
// matches this helper version. The togo socket label comes from
// TOGO_TMUX_LABEL when set, which only tests do.
func NewService(_ context.Context, home string) (*Service, error) {
	conf := filepath.Join(home, "tmux.conf")
	if err := state.WriteFile(conf, []byte(tmuxConf)); err != nil {
		return nil, fmt.Errorf("tmux config: %w", err)
	}
	label := cmp.Or(os.Getenv("TOGO_TMUX_LABEL"), "togo")
	return &Service{
		Tmux:     &tmux.Client{Bin: "tmux", Socket: []string{"-L", label}, Config: conf},
		User:     tmux.NewDefault(),
		Recent:   &state.Recent{File: filepath.Join(home, "recent.json")},
		LockFile: filepath.Join(home, "run", "bib.lock"),
		Shell:    cmp.Or(os.Getenv("SHELL"), "/bin/sh"),
	}, nil
}

// List returns managed sessions ordered by bib, then unmanaged sessions
// ordered by name.
func (s *Service) List(ctx context.Context) ([]Session, error) {
	if err := s.checkTmux(ctx); err != nil {
		return nil, err
	}
	managed, err := s.listManaged(ctx)
	if err != nil {
		return nil, err
	}
	return append(managed, s.listUnmanaged(ctx)...), nil
}

func (s *Service) listManaged(ctx context.Context) ([]Session, error) {
	infos, err := s.Tmux.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	out := []Session{}
	for _, info := range infos {
		// Anything else on the togo socket was not made by Create, for
		// example by someone running tmux -L togo by hand, and could not
		// be described as a valid managed session.
		if managedIDPattern.MatchString(info.Name) && validBib(info.Bib) {
			out = append(out, managedSession(info))
		}
	}
	slices.SortFunc(out, func(a, b Session) int { return strings.Compare(*a.Bib, *b.Bib) })
	return out, nil
}

// listUnmanaged returns the user's default-socket sessions. A failing
// default server counts as having none.
func (s *Service) listUnmanaged(ctx context.Context) []Session {
	if s.User == nil {
		return nil
	}
	infos, err := s.User.ListSessions(ctx)
	if err != nil {
		// The user's own server is outside the helper's control (an old
		// server left running across a tmux upgrade fails every command),
		// and must not hide the managed sessions.
		return nil
	}
	slices.SortFunc(infos, func(a, b tmux.SessionInfo) int { return strings.Compare(a.Name, b.Name) })
	out := make([]Session, 0, len(infos))
	for _, info := range infos {
		out = append(out, unmanagedSession(info))
	}
	return out
}

// Kill ends a managed session.
func (s *Service) Kill(ctx context.Context, id string) error {
	if err := requireManaged(id); err != nil {
		return err
	}
	if err := s.checkTmux(ctx); err != nil {
		return err
	}
	if err := s.Tmux.KillSession(ctx, id); err != nil {
		return notFoundOr(err, id)
	}
	return nil
}

// Rename sets a managed session's display name and returns the session.
// The name follows Create's rules, except that blank is rejected.
func (s *Service) Rename(ctx context.Context, id, name string) (Session, error) {
	if err := requireManaged(id); err != nil {
		return Session{}, err
	}
	name, err := normalizeName(name)
	if err != nil {
		return Session{}, err
	}
	if name == "" {
		return Session{}, &InvalidParamsError{Msg: "name must not be empty"}
	}
	if err := s.checkTmux(ctx); err != nil {
		return Session{}, err
	}
	if err := s.Tmux.SetOption(ctx, id, optionName, name); err != nil {
		return Session{}, notFoundOr(err, id)
	}
	return s.find(ctx, id)
}

// AttachCommand returns the argv that attaches a terminal to the session.
// It works for unmanaged sessions too, since attaching changes nothing.
func (s *Service) AttachCommand(ctx context.Context, id string) ([]string, error) {
	sess, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if sess.Managed {
		return s.Tmux.AttachArgv(sess.ID), nil
	}
	return s.User.AttachArgv(sess.Name), nil
}

// Resolve finds a session by id or by bib, written with or without the
// leading zero ("4" or "04").
func (s *Service) Resolve(ctx context.Context, bibOrID string) (Session, error) {
	if _, err := ValidateID(bibOrID); err == nil {
		return s.find(ctx, bibOrID)
	}
	n, err := strconv.Atoi(bibOrID)
	if !isBibNumber(bibOrID) || err != nil || n < 1 {
		return Session{}, &InvalidParamsError{Msg: fmt.Sprintf("%q is not a session number or id", bibOrID)}
	}
	bib := fmt.Sprintf("%02d", n)
	if err := s.checkTmux(ctx); err != nil {
		return Session{}, err
	}
	sessions, err := s.listManaged(ctx)
	if err != nil {
		return Session{}, err
	}
	for _, sess := range sessions {
		if *sess.Bib == bib {
			return sess, nil
		}
	}
	return Session{}, fmt.Errorf("%w: number %s", ErrNotFound, bib)
}

// find looks id up on the one socket it can live on, so a broken default
// server never hides a managed session.
func (s *Service) find(ctx context.Context, id string) (Session, error) {
	managed, err := ValidateID(id)
	if err != nil {
		return Session{}, err
	}
	if err := s.checkTmux(ctx); err != nil {
		return Session{}, err
	}
	var sessions []Session
	if managed {
		if sessions, err = s.listManaged(ctx); err != nil {
			return Session{}, err
		}
	} else {
		sessions = s.listUnmanaged(ctx)
	}
	for _, sess := range sessions {
		if sess.ID == id {
			return sess, nil
		}
	}
	return Session{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// checkTmux verifies the tmux version once per Service. Results that will
// not change while the helper runs (missing, too old, supported) are cached;
// other failures, such as a cancelled context, are retried next call.
func (s *Service) checkTmux(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tmuxChecked {
		return s.tmuxErr
	}
	major, minor, raw, err := s.Tmux.Version(ctx)
	switch {
	case errors.Is(err, tmux.ErrNotInstalled):
		s.tmuxErr = &TmuxUnavailableError{}
	case err != nil && raw == "":
		return fmt.Errorf("tmux version: %w", err)
	case err != nil || major < requiredMajor || major == requiredMajor && minor < requiredMinor:
		s.tmuxErr = &TmuxUnavailableError{Found: strings.TrimPrefix(raw, "tmux ")}
	}
	s.tmuxChecked = true
	return s.tmuxErr
}

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	return newID()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) shell() string {
	return cmp.Or(s.Shell, "/bin/sh")
}

// requireManaged rejects ids that are malformed or unmanaged from the id
// alone, before any tmux call.
func requireManaged(id string) error {
	managed, err := ValidateID(id)
	if err != nil {
		return err
	}
	if !managed {
		return fmt.Errorf("%w: %s", ErrNotManaged, id)
	}
	return nil
}

func notFoundOr(err error, id string) error {
	if errors.Is(err, tmux.ErrNoSession) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return fmt.Errorf("session %s: %w", id, err)
}

// isBibNumber reports whether s is one or two ASCII digits. strconv.Atoi
// alone would also accept signs such as "+1".
func isBibNumber(s string) bool {
	if len(s) < 1 || len(s) > 2 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validBib(bib string) bool {
	n, err := strconv.Atoi(bib)
	return len(bib) == 2 && isBibNumber(bib) && err == nil && n >= 1
}

func managedSession(info tmux.SessionInfo) Session {
	created := info.Created.UTC()
	if t, err := time.Parse(time.RFC3339, info.CreatedTag); err == nil {
		created = t.UTC()
	}
	bib := info.Bib
	return Session{
		ID:              info.Name,
		Bib:             &bib,
		Name:            info.Title,
		Agent:           info.Agent,
		Cwd:             info.Path,
		CreatedAt:       created,
		Status:          status(info),
		AttachedClients: info.Attached,
		Managed:         true,
	}
}

func unmanagedSession(info tmux.SessionInfo) Session {
	return Session{
		ID:              unmanagedID(info.Name),
		Name:            info.Name,
		Agent:           "shell",
		Cwd:             info.Path,
		CreatedAt:       info.Created.UTC(),
		Status:          status(info),
		AttachedClients: info.Attached,
	}
}

func status(info tmux.SessionInfo) string {
	if info.Dead {
		return statusExited
	}
	return statusIdle
}
