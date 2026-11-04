// Package api maps the protocol v1 session, agent and filesystem methods
// onto the helper's domain packages.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
)

// API is the rpc.Handler for every method except hello.
type API struct {
	Sessions *session.Service
	Recent   *state.Recent
	// Home is the home directory returned by fs.recentDirs when nothing is recorded.
	Home string
	// Logf receives the details of internal errors, which never reach the
	// client. Nil discards them.
	Logf func(format string, args ...any)
}

// Handle dispatches one protocol method.
func (a *API) Handle(ctx context.Context, method string, params json.RawMessage) (any, *rpc.Error) {
	var (
		result any
		err    error
	)
	switch method {
	case "session.list":
		result, err = a.sessionList(ctx)
	case "session.create":
		result, err = a.sessionCreate(ctx, params)
	case "session.kill":
		result, err = a.sessionKill(ctx, params)
	case "session.rename":
		result, err = a.sessionRename(ctx, params)
	case "session.attachCommand":
		result, err = a.sessionAttachCommand(ctx, params)
	case "agents.list":
		result = a.agentsList()
	case "fs.recentDirs":
		result, err = a.recentDirs()
	case "fs.listDir":
		result, err = a.listDir(params)
	default:
		return nil, rpc.NewError(rpc.CodeMethodNotFound, nil)
	}
	if err != nil {
		return nil, a.mapError(method, err)
	}
	return result, nil
}

func (a *API) sessionList(ctx context.Context) (any, error) {
	sessions, err := a.Sessions.List(ctx)
	if err != nil {
		return nil, err
	}
	if sessions == nil {
		sessions = []session.Session{}
	}
	return map[string]any{"sessions": sessions}, nil
}

func (a *API) sessionCreate(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Agent string `json:"agent"`
		Cwd   string `json:"cwd"`
		Name  string `json:"name"`
		Cols  int    `json:"cols"`
		Rows  int    `json:"rows"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	return a.Sessions.Create(ctx, session.CreateOpts{Agent: p.Agent, Cwd: p.Cwd, Name: p.Name, Cols: p.Cols, Rows: p.Rows})
}

func (a *API) sessionKill(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := a.Sessions.Kill(ctx, p.ID); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

func (a *API) sessionRename(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	return a.Sessions.Rename(ctx, p.ID, p.Name)
}

func (a *API) sessionAttachCommand(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if _, err := session.ValidateID(p.ID); err != nil {
		return nil, err
	}
	argv, err := a.Sessions.AttachCommand(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"argv": argv}, nil
}

func (a *API) agentsList() any {
	// A fresh login-shell PATH lookup per call: the user may install an agent while the app is open.
	path := a.Sessions.Path
	if path == "" {
		path = state.LoginPath(context.Background())
	}
	type entry struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Installed bool   `json:"installed"`
	}
	list := make([]entry, 0, len(agents.Registry))
	for _, ag := range agents.Registry {
		list = append(list, entry{ag.ID, ag.Name, agents.Installed(ag, path)})
	}
	return map[string]any{"agents": list}
}

func (a *API) recentDirs() (any, error) {
	dirs, err := a.Recent.List()
	if err != nil {
		return nil, err
	}
	if len(dirs) == 0 {
		dirs = []string{a.Home}
	}
	return map[string]any{"dirs": dirs}, nil
}

func (a *API) listDir(raw json.RawMessage) (any, error) {
	var p struct {
		Path       *string `json:"path"`
		ShowHidden bool    `json:"showHidden"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Path == nil {
		return nil, &session.InvalidParamsError{Msg: "path is required"}
	}
	dir, entries, err := fsx.ListDir(*p.Path, p.ShowHidden)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []fsx.Entry{}
	}
	return map[string]any{"path": dir, "entries": entries}, nil
}

// decode reads a params object into v. Absent or null params count as {}
// and unknown fields are ignored. Failures are *session.InvalidParamsError.
func decode(raw json.RawMessage, v any) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		raw = []byte("{}")
	}
	if raw[0] != '{' {
		return &session.InvalidParamsError{Msg: "params must be an object"}
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return &session.InvalidParamsError{Msg: err.Error()}
	}
	return nil
}

func (a *API) mapError(method string, err error) *rpc.Error {
	var (
		invalid *session.InvalidParamsError
		tmuxErr *session.TmuxUnavailableError
	)
	switch {
	case errors.Is(err, session.ErrNotFound):
		return rpc.NewError(rpc.CodeNotFound, nil)
	case errors.Is(err, session.ErrNotManaged):
		return rpc.NewError(rpc.CodeNotManaged, nil)
	case errors.Is(err, fsx.ErrInvalidPath):
		return rpc.NewError(rpc.CodeInvalidPath, nil)
	case errors.As(err, &tmuxErr):
		var found *string
		if tmuxErr.Found != "" {
			found = &tmuxErr.Found
		}
		return rpc.NewError(rpc.CodeTmuxUnavailable, map[string]any{"found": found, "required": "3.2"})
	case errors.As(err, &invalid):
		return rpc.InvalidParams(invalid.Msg)
	}
	if a.Logf != nil {
		a.Logf("%s: %s", method, fmt.Sprint(err))
	}
	return rpc.NewError(rpc.CodeInternal, nil)
}
