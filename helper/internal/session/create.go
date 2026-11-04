package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tmux"
)

const (
	maxNameRunes = 64
	defaultCols  = 80
	minCols      = 20
	maxCols      = 500
	defaultRows  = 24
	minRows      = 5
	maxRows      = 300

	optionBib     = "@togo_bib"
	optionAgent   = "@togo_agent"
	optionName    = "@togo_name"
	optionCreated = "@togo_created"
)

// CreateOpts are the inputs to Create. Zero values select the defaults: the
// shell agent, the home directory, the agent's display name and 80x24.
type CreateOpts struct {
	Agent, Cwd, Name string
	Cols, Rows       int
}

// Create starts a managed session and returns it as List would. Bad input
// is an *InvalidParamsError, except a bad Cwd, which wraps
// fsx.ErrInvalidPath.
func (s *Service) Create(ctx context.Context, o CreateOpts) (Session, error) {
	agentID := o.Agent
	if agentID == "" {
		agentID = "shell"
	}
	a, ok := agents.Lookup(agentID)
	if !ok {
		return Session{}, &InvalidParamsError{Msg: fmt.Sprintf("unknown agent %q", agentID)}
	}
	name, err := normalizeName(o.Name)
	if err != nil {
		return Session{}, err
	}
	if name == "" {
		name = a.Name
	}
	cols, err := sizeOrDefault("cols", o.Cols, defaultCols, minCols, maxCols)
	if err != nil {
		return Session{}, err
	}
	rows, err := sizeOrDefault("rows", o.Rows, defaultRows, minRows, maxRows)
	if err != nil {
		return Session{}, err
	}
	cwd := o.Cwd
	if cwd == "" {
		cwd = "~"
	}
	dir, err := fsx.ValidateDir(cwd)
	if err != nil {
		return Session{}, err
	}
	if err := s.checkTmux(ctx); err != nil {
		return Session{}, err
	}
	path := s.Path
	if path == "" {
		path = state.LoginPath(ctx)
	}
	if !agents.Installed(a, path) {
		return Session{}, &InvalidParamsError{Msg: fmt.Sprintf("agent %q is not installed", a.ID)}
	}

	created := s.now().UTC().Truncate(time.Second)
	var id, bib string
	err = withLock(ctx, s.LockFile, func() error {
		var err error
		if bib, err = s.nextBib(ctx); err != nil {
			return err
		}
		// Recording the folder before the session exists means a failed
		// write never leaves behind a session the caller was told failed.
		if err := s.Recent.Add(dir); err != nil {
			return err
		}
		id = s.newID()
		return s.Tmux.NewSession(ctx, tmux.NewSessionOpts{
			Name:    id,
			Dir:     dir,
			Cols:    cols,
			Rows:    rows,
			Command: agentCommand(a, s.shell()),
			Options: map[string]string{
				optionBib:     bib,
				optionAgent:   a.ID,
				optionName:    name,
				optionCreated: created.Format(time.RFC3339),
			},
			Env: map[string]string{"TOGO_SESSION": id, "TOGO_BIB": bib},
		})
	})
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	sess, err := s.find(ctx, id)
	if errors.Is(err, ErrNotFound) {
		// The pane's process already exited and tmux destroyed the
		// session. The caller still gets the session it created.
		cwd, evalErr := filepath.EvalSymlinks(dir)
		if evalErr != nil {
			cwd = dir
		}
		return Session{
			ID:        id,
			Bib:       &bib,
			Name:      name,
			Agent:     a.ID,
			Cwd:       cwd,
			CreatedAt: created,
			Status:    statusExited,
			Managed:   true,
		}, nil
	}
	return sess, err
}

func (s *Service) nextBib(ctx context.Context) (string, error) {
	infos, err := s.Tmux.ListSessions(ctx)
	if err != nil {
		return "", err
	}
	used := make(map[string]bool, len(infos))
	for _, info := range infos {
		used[info.Bib] = true
	}
	return lowestFreeBib(used)
}

// agentCommand returns the initial pane argv. The shell agent gets none, so
// tmux starts its default shell. Other agents run in an interactive login
// shell, which sets up PATH the way the user's terminal does, and fall back
// to a login shell when they exit so the pane stays usable. Only registry
// strings ever enter the -c text.
func agentCommand(a agents.Agent, shell string) []string {
	if a.Command == "" {
		return nil
	}
	return []string{shell, "-lic", a.Command + `; exec "$SHELL" -l`}
}

// normalizeName trims Unicode whitespace and checks the result is at most 64
// runes of valid UTF-8 with no control characters. An empty result is
// returned as "" for the caller to default or reject.
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if !utf8.ValidString(name) {
		return "", &InvalidParamsError{Msg: "name is not valid UTF-8"}
	}
	if n := utf8.RuneCountInString(name); n > maxNameRunes {
		return "", &InvalidParamsError{Msg: fmt.Sprintf("name is %d characters, the limit is %d", n, maxNameRunes)}
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", &InvalidParamsError{Msg: "name contains a control character"}
	}
	return name, nil
}

func sizeOrDefault(field string, v, def, lo, hi int) (int, error) {
	if v == 0 {
		return def, nil
	}
	if v < lo || v > hi {
		return 0, &InvalidParamsError{Msg: fmt.Sprintf("%s must be 0 or %d-%d, got %d", field, lo, hi, v)}
	}
	return v, nil
}
