package gitx

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	defaultLogLimit = 50
	maxLogLimit     = 200
)

// Service binds the git methods to the sessions whose directories they read
// and, for managed sessions, write.
type Service struct {
	Sessions Resolver
	Git      Git

	// writes runs one git write at a time, so the status a write checks is
	// not changed under it by another write from this helper.
	writes sync.Mutex
}

// Methods returns the git.* handlers for API.Ext.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{
		wire.MethodGitStatus:   s.status,
		wire.MethodGitDiff:     s.diff,
		wire.MethodGitLog:      s.log,
		wire.MethodGitBranches: s.branches,
		wire.MethodGitFiles:    s.files,
		wire.MethodGitStage:    s.stage,
		wire.MethodGitUnstage:  s.unstage,
		wire.MethodGitDiscard:  s.discard,
		wire.MethodGitCommit:   s.commit,
	}
}

type idParams struct {
	ID string `json:"id"`
}

// begin decodes params into v and returns the session directory for id.
func (s *Service) begin(ctx context.Context, raw json.RawMessage, v any, id func() string) (string, error) {
	if err := api.Decode(raw, v); err != nil {
		return "", err
	}
	return SessionDir(ctx, s.Sessions, id())
}

func (s *Service) status(ctx context.Context, raw json.RawMessage) (any, error) {
	var p idParams
	dir, err := s.begin(ctx, raw, &p, func() string { return p.ID })
	if err != nil {
		return nil, err
	}
	return s.Git.Status(ctx, dir)
}

func (s *Service) branches(ctx context.Context, raw json.RawMessage) (any, error) {
	var p idParams
	dir, err := s.begin(ctx, raw, &p, func() string { return p.ID })
	if err != nil {
		return nil, err
	}
	return s.Git.Branches(ctx, dir)
}

type diffParams struct {
	idParams
	Path   string  `json:"path"`
	Staged bool    `json:"staged"`
	Ref    string  `json:"ref"`
	Commit *string `json:"commit"`
}

func (s *Service) diff(ctx context.Context, raw json.RawMessage) (any, error) {
	var p diffParams
	dir, err := s.begin(ctx, raw, &p, func() string { return p.ID })
	if err != nil {
		return nil, err
	}
	o := DiffOptions{Staged: p.Staged, Ref: p.Ref}
	if p.Commit != nil {
		if *p.Commit == "" {
			return nil, &session.InvalidParamsError{Msg: "commit must not be empty"}
		}
		o.Commit = *p.Commit
	}
	if p.Path != "" {
		if o.Path, err = CleanRel(p.Path); err != nil {
			return nil, err
		}
	}
	return s.Git.Diff(ctx, dir, o)
}

type logParams struct {
	idParams
	Ref   string `json:"ref"`
	Limit *int   `json:"limit"`
	// Graph asks for topological order and changed-file counts (see LogOptions).
	Graph bool `json:"graph"`
}

func (s *Service) log(ctx context.Context, raw json.RawMessage) (any, error) {
	var p logParams
	dir, err := s.begin(ctx, raw, &p, func() string { return p.ID })
	if err != nil {
		return nil, err
	}
	limit := defaultLogLimit
	if p.Limit != nil {
		if *p.Limit < 1 || *p.Limit > maxLogLimit {
			return nil, &session.InvalidParamsError{Msg: "limit must be 1 to 200"}
		}
		limit = *p.Limit
	}
	commits, err := s.Git.Log(ctx, dir, p.Ref, limit, LogOptions{Graph: p.Graph})
	if err != nil {
		return nil, err
	}
	return map[string]any{"commits": commits}, nil
}

// writeDir returns the directory of a managed session. Unmanaged sessions
// are refused from the id alone when it says so, else once resolved.
func (s *Service) writeDir(ctx context.Context, id string) (string, error) {
	if managed, err := session.ValidateID(id); err == nil && !managed {
		return "", session.ErrNotManaged
	}
	sess, err := s.Sessions.Resolve(ctx, id)
	if err != nil {
		return "", err
	}
	if !sess.Managed {
		return "", session.ErrNotManaged
	}
	return SessionDir(ctx, s.Sessions, id)
}

// protocolError turns a refused write into its -32006 reply.
func protocolError(err error) error {
	var ce *ConflictError
	if errors.As(err, &ce) {
		data := map[string]any{"reason": ce.Reason}
		if len(ce.Changed) > 0 {
			data["changed"] = ce.Changed
		}
		if ce.Hash != "" {
			data["hash"] = ce.Hash
		}
		return &api.CodeError{Code: rpc.CodeConflict, Data: data}
	}
	return err
}

type pathsParams struct {
	idParams
	Paths []string                `json:"paths"`
	Seen  map[string]wire.GitSeen `json:"seen"`
}

type pathsWrite func(ctx context.Context, dir string, paths []string, seen map[string]wire.GitSeen) (Status, error)

func (s *Service) stage(ctx context.Context, raw json.RawMessage) (any, error) {
	return s.writePaths(ctx, raw, s.Git.Stage)
}

func (s *Service) unstage(ctx context.Context, raw json.RawMessage) (any, error) {
	return s.writePaths(ctx, raw, s.Git.Unstage)
}

func (s *Service) discard(ctx context.Context, raw json.RawMessage) (any, error) {
	return s.writePaths(ctx, raw, s.Git.Discard)
}

func (s *Service) writePaths(ctx context.Context, raw json.RawMessage, write pathsWrite) (any, error) {
	var p pathsParams
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	if managed, err := session.ValidateID(p.ID); err == nil && !managed {
		return nil, session.ErrNotManaged
	}
	paths, seen, err := cleanWritePaths(p.Paths, p.Seen)
	if err != nil {
		return nil, err
	}
	dir, err := s.writeDir(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	st, err := write(ctx, dir, paths, seen)
	if err != nil {
		return nil, protocolError(err)
	}
	return st, nil
}

// cleanWritePaths validates the paths and seen keys of a git write and
// returns them cleaned, paths without repeats.
func cleanWritePaths(paths []string, seen map[string]wire.GitSeen) ([]string, map[string]wire.GitSeen, error) {
	if len(paths) == 0 || len(paths) > wire.MaxGitPaths {
		return nil, nil, &session.InvalidParamsError{Msg: "paths must list 1 to 1000 files"}
	}
	clean := func(p string) (string, error) {
		if !wire.ValidWritePath(p) {
			return "", &session.InvalidParamsError{Msg: "each path must name one file of at most 4096 bytes"}
		}
		return CleanRel(p)
	}
	var out []string
	dup := map[string]bool{}
	for _, p := range paths {
		c, err := clean(p)
		if err != nil {
			return nil, nil, err
		}
		if !dup[c] {
			dup[c] = true
			out = append(out, c)
		}
	}
	cleanSeen := map[string]wire.GitSeen{}
	for k, v := range seen {
		c, err := clean(k)
		if err != nil {
			return nil, nil, err
		}
		cleanSeen[c] = v
	}
	return out, cleanSeen, nil
}

type commitParams struct {
	idParams
	Message *string `json:"message"`
	Amend   *bool   `json:"amend"`
	Head    string  `json:"head"`
}

func (s *Service) commit(ctx context.Context, raw json.RawMessage) (any, error) {
	var p commitParams
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	if managed, err := session.ValidateID(p.ID); err == nil && !managed {
		return nil, session.ErrNotManaged
	}
	switch {
	case p.Message == nil || p.Amend == nil:
		return nil, &session.InvalidParamsError{Msg: "message and amend are required"}
	case *p.Amend && p.Head == "":
		return nil, &session.InvalidParamsError{Msg: "amend needs the head commit it replaces"}
	case p.Head != "" && !wire.ValidCommitHash(p.Head):
		return nil, &session.InvalidParamsError{Msg: "head must be a full commit hash"}
	}
	if err := checkMessage(*p.Message); err != nil {
		return nil, err
	}
	dir, err := s.writeDir(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	res, err := s.Git.Commit(ctx, dir, *p.Message, *p.Amend, p.Head)
	if err != nil {
		return nil, protocolError(err)
	}
	return res, nil
}

// checkMessage applies the commit message rules: valid UTF-8 of 1 to
// wire.MaxCommitBytes bytes, a first line with text, and no control
// characters other than newline and tab.
func checkMessage(m string) error {
	first, _, _ := strings.Cut(m, "\n")
	switch {
	case m == "" || len(m) > wire.MaxCommitBytes:
		return &session.InvalidParamsError{Msg: "message must be 1 to 65536 bytes"}
	case !utf8.ValidString(m):
		return &session.InvalidParamsError{Msg: "message is not valid UTF-8"}
	case strings.TrimSpace(first) == "":
		return &session.InvalidParamsError{Msg: "the first line of the message is empty"}
	}
	for _, c := range m {
		if c != '\n' && c != '\t' && (c < 0x20 || (c >= 0x7f && c <= 0x9f)) {
			return &session.InvalidParamsError{Msg: "message contains a control character"}
		}
	}
	return nil
}
