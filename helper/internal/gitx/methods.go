package gitx

import (
	"context"
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	defaultLogLimit = 50
	maxLogLimit     = 200
)

// Service binds the git methods to the sessions whose directories they read.
type Service struct {
	Sessions Resolver
	Git      Git
}

// Methods returns the git.* handlers for API.Ext.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{
		wire.MethodGitStatus:   s.status,
		wire.MethodGitDiff:     s.diff,
		wire.MethodGitLog:      s.log,
		wire.MethodGitBranches: s.branches,
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
	Path   string `json:"path"`
	Staged bool   `json:"staged"`
	Ref    string `json:"ref"`
}

func (s *Service) diff(ctx context.Context, raw json.RawMessage) (any, error) {
	var p diffParams
	dir, err := s.begin(ctx, raw, &p, func() string { return p.ID })
	if err != nil {
		return nil, err
	}
	o := DiffOptions{Staged: p.Staged, Ref: p.Ref}
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
	commits, err := s.Git.Log(ctx, dir, p.Ref, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"commits": commits}, nil
}
