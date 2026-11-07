// Package integrations serves the app's setup surface: integrations.status
// reports each harness's setup state (the rows `togo doctor` prints), and
// integrations.install and integrations.uninstall run the helper's own hook
// installer for the harnesses the phone names. The daemon runs them as the
// user. The phone sends only registry ids, never paths.
package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/hooks"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
)

// Protocol names.
const (
	Capability      = "integrations.v1"
	MethodStatus    = "integrations.status"
	MethodInstall   = "integrations.install"
	MethodUninstall = "integrations.uninstall"
)

// seenEvery bounds how often one harness's last-hook time is written.
const seenEvery = time.Minute

// Service runs the installer and keeps the last-hook record.
type Service struct {
	// Options resolves the harness config roots; New fills it.
	Options func(ctx context.Context) (hooks.Options, error)
	Now     func() time.Time
	Logf    func(format string, args ...any)

	mu      sync.Mutex // one install at a time; guards seen
	seen    hooks.Seen
	written map[string]time.Time
}

// New returns the service for the daemon: TOGO_HOME is home and binary is
// the helper the hooks run.
func New(home, binary string) *Service {
	return &Service{Options: func(ctx context.Context) (hooks.Options, error) {
		user, err := os.UserHomeDir()
		if err != nil {
			return hooks.Options{}, err
		}
		o := hooks.DefaultOptions(os.Getenv, user, home, binary)
		path, fallbacks := state.LoginPath(ctx), agents.FallbackDirs(user)
		o.HasBinary = func(command string) bool {
			_, _, ok := agents.Find(command, path, fallbacks)
			return ok
		}
		return o, nil
	}}
}

// StatusResult is integrations.status's result.
type StatusResult struct {
	Integrations []hooks.Report `json:"integrations"`
}

// ChangeParams are integrations.install's and integrations.uninstall's.
// Install with no agents sets up every installed harness.
type ChangeParams struct {
	Agents []string `json:"agents,omitempty"`
	Block  bool     `json:"block,omitempty"`
}

// Outcome is one harness's result. State is installed, unchanged,
// removed, skipped or refused (with Error).
type Outcome struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Path   string `json:"path,omitempty"`
	Backup string `json:"backup,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ChangeResult is what install and uninstall return: each harness's
// outcome and the rows after the change.
type ChangeResult struct {
	Results      []Outcome      `json:"results"`
	Integrations []hooks.Report `json:"integrations"`
}

// Methods returns the integrations.* methods for api.API.Ext.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{
		MethodStatus: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct{}
			if err := decode(raw, &p); err != nil {
				return nil, err
			}
			o, err := s.Options(ctx)
			if err != nil {
				return nil, err
			}
			return StatusResult{Integrations: s.reports(o)}, nil
		},
		MethodInstall: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return s.change(ctx, raw, true)
		},
		MethodUninstall: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return s.change(ctx, raw, false)
		},
	}
}

func (s *Service) change(ctx context.Context, raw json.RawMessage, install bool) (any, error) {
	var p ChangeParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := validate(p.Agents, install); err != nil {
		return nil, err
	}
	o, err := s.Options(ctx)
	if err != nil {
		return nil, err
	}
	o.Block = p.Block
	ids := p.Agents
	if len(ids) == 0 {
		ids = hooks.Default(o)
	}
	s.mu.Lock()
	var rs []hooks.Result
	if install {
		rs = hooks.Install(o, ids)
	} else {
		rs = hooks.Uninstall(o, ids)
	}
	s.mu.Unlock()
	out := ChangeResult{Results: make([]Outcome, 0, len(rs))}
	for _, r := range rs {
		oc := Outcome{ID: r.Agent, State: r.State, Path: r.Path, Backup: r.Backup}
		if r.Err != nil {
			oc.Error = r.Err.Error()
		}
		out.Results = append(out.Results, oc)
	}
	out.Integrations = s.reports(o)
	return out, nil
}

// decode is api.Decode that also refuses unknown fields: the phone sends
// ids and a flag, nothing else.
func decode(raw json.RawMessage, v any) error {
	if err := api.Decode(raw, v); err != nil {
		return err
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &session.InvalidParamsError{Msg: err.Error()}
	}
	return nil
}

// validate takes registry ids only: no duplicates, at most every harness,
// and uninstall must name at least one.
func validate(ids []string, install bool) error {
	if !install && len(ids) == 0 {
		return &session.InvalidParamsError{Msg: "agents is required"}
	}
	if len(ids) > len(hooks.Agents) {
		return &session.InvalidParamsError{Msg: "too many agents"}
	}
	for i, id := range ids {
		if !hooks.Known(id) {
			return &session.InvalidParamsError{Msg: "unknown agent", Reason: "unknown_agent"}
		}
		if slices.Contains(ids[:i], id) {
			return &session.InvalidParamsError{Msg: "agent named twice"}
		}
	}
	return nil
}

func (s *Service) reports(o hooks.Options) []hooks.Report {
	s.mu.Lock()
	seen := s.loadSeen(o.TogoHome)
	s.mu.Unlock()
	return hooks.Reports(o, seen)
}

// loadSeen returns the in-memory record, read from disk the first time.
// Callers hold s.mu.
func (s *Service) loadSeen(togoHome string) hooks.Seen {
	if s.seen == nil {
		s.seen = hooks.LoadSeen(togoHome)
		s.written = map[string]time.Time{}
	}
	return maps.Clone(s.seen)
}
