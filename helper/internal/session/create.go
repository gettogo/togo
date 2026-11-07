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

	"github.com/gettogo/togo/helper/internal/fsx"
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
	optionMuted   = "@togo_muted"
)

// CreateOpts are the inputs to Create. Zero values select the defaults: the
// shell agent, the home directory, the agent's display name and 80x24.
// Program names the program for wire.AgentCustom; Args are argv entries
// passed to the agent after its program, never read as shell text.
type CreateOpts struct {
	Agent, Cwd, Name string
	Cols, Rows       int
	Program          string
	Args             []string
	// Muted sets @togo_muted from the start.
	Muted bool
}

// Create starts a managed session and returns it as List would. Bad input
// is an *InvalidParamsError, except a bad Cwd, which wraps
// fsx.ErrInvalidPath.
func (s *Service) Create(ctx context.Context, o CreateOpts) (Session, error) {
	name, err := normalizeName(o.Name)
	if err != nil {
		return Session{}, err
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
	a, launch, err := ValidateLaunch(o.Agent, o.Program, o.Args, s.loginPath(ctx), s.fallbackDirs())
	if err != nil {
		return Session{}, err
	}
	if name == "" {
		name = a.Name
	}
	if err := s.checkTmux(ctx); err != nil {
		return Session{}, err
	}

	created := s.now().UTC().Truncate(time.Second)
	options := map[string]string{
		optionAgent:   a.ID,
		optionName:    name,
		optionCreated: created.Format(time.RFC3339),
	}
	if o.Muted {
		options[optionMuted] = "1"
	}
	if argv := launch.Argv(); argv != nil {
		if options[optionArgv], err = encodeArgv(argv); err != nil {
			return Session{}, err
		}
	}
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
		options[optionBib] = bib
		run := launch.WithEndpoint(a.ID, id)
		err = s.Tmux.NewSession(ctx, tmux.NewSessionOpts{
			Name:    id,
			Dir:     dir,
			Cols:    cols,
			Rows:    rows,
			Command: launchWrapper(s.shell(), s.TogoPath, run),
			Options: options,
			Env:     withEnv(s.sessionEnv(id, bib, a.ID), run.Env),
		})
		if err == nil {
			s.remember(ManifestEntry{ID: id, Bib: bib, Name: name, Agent: a.ID, Cwd: dir, Command: launch.Argv(), CreatedAt: created})
		}
		return err
	})
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return s.started(ctx, id, bib, name, a.ID, dir, created, launch.Argv())
}

// started returns a session just started in tmux as List would. When the
// pane's process already exited and tmux destroyed the session, the caller
// still gets the session it started, as exited.
func (s *Service) started(ctx context.Context, id, bib, name, agent, dir string, created time.Time, argv []string) (Session, error) {
	sess, err := s.find(ctx, id)
	if errors.Is(err, ErrNotFound) {
		cwd, evalErr := filepath.EvalSymlinks(dir)
		if evalErr != nil {
			cwd = dir
		}
		return Session{
			ID:        id,
			Bib:       &bib,
			Name:      name,
			Agent:     agent,
			Cwd:       cwd,
			CreatedAt: created,
			Status:    statusExited,
			Managed:   true,
			Command:   argv,
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
