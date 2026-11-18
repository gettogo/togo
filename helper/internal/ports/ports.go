// Package ports finds the TCP ports that processes in a session's process
// tree are listening on, so the phone can forward and preview a dev server
// an agent started.
package ports

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// lookupTimeout bounds one ports.list call, including the ps, lsof and tmux
// processes it starts. lsof alone can take several seconds on a loaded
// machine, so the bound is generous. A variable so tests can shorten it.
var lookupTimeout = 15 * time.Second

// ErrTimeout is wrapped by a ports.list that ran past lookupTimeout.
var ErrTimeout = errors.New("ports lookup timed out")

// Listener is one TCP socket in the LISTEN state.
type Listener struct {
	Port    int    `json:"port"`
	Address string `json:"address"`
	PID     int    `json:"pid"`
	Command string `json:"command"`
}

// Resolver finds a session by id.
type Resolver interface {
	Resolve(ctx context.Context, bibOrID string) (session.Session, error)
}

// PaneSource lists the pane process ids of a tmux session.
type PaneSource interface {
	PanePIDs(ctx context.Context, session string) ([]int, error)
}

// Service answers ports.list.
type Service struct {
	Sessions Resolver
	Panes    PaneSource
}

// List returns the listeners held by the session's process tree, ordered by
// port. Unmanaged sessions are refused: they live on another tmux server.
func (s *Service) List(ctx context.Context, id string) ([]Listener, error) {
	sess, err := s.Sessions.Resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	if !sess.Managed {
		return nil, session.ErrNotManaged
	}
	lookupCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	found, err := s.lookup(lookupCtx, sess.ID)
	// A killed lsof reports only its signal, so the deadline is read from
	// the context.
	if err != nil && ctx.Err() == nil && errors.Is(lookupCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("%w after %v: %v", ErrTimeout, lookupTimeout, err)
	}
	return found, err
}

func (s *Service) lookup(ctx context.Context, id string) ([]Listener, error) {
	roots, err := s.Panes.PanePIDs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("pane pids: %w", err)
	}
	if len(roots) == 0 {
		return []Listener{}, nil
	}
	table, err := readProcessTable(ctx)
	if err != nil {
		return nil, err
	}
	found, err := listeners(ctx, descendants(table, roots))
	if err != nil {
		return nil, err
	}
	return normalize(found), nil
}

// normalize keeps one listener per port and orders by port. A port held by
// several processes (workers, a reloader) or bound to several addresses is
// one thing to open: the address that a forward to loopback reaches comes
// first, then the lowest pid, which is the parent of its workers.
func normalize(in []Listener) []Listener {
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b Listener) int {
		return cmp.Or(cmp.Compare(a.Port, b.Port), cmp.Compare(addressRank(a.Address), addressRank(b.Address)),
			cmp.Compare(a.Address, b.Address), cmp.Compare(a.PID, b.PID))
	})
	out = slices.CompactFunc(out, func(a, b Listener) bool { return a.Port == b.Port })
	if out == nil {
		out = []Listener{}
	}
	return out
}

// addressRank orders the addresses of one port: IPv4 loopback, every
// address, IPv6 loopback, then the rest.
func addressRank(a string) int {
	switch a {
	case "127.0.0.1":
		return 0
	case "*", "0.0.0.0":
		return 1
	case "::1", "[::1]":
		return 2
	case "::", "[::]":
		return 3
	}
	return 4
}

// Methods returns the protocol methods this package serves, for API.Ext.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{wire.MethodPortsList: s.portsList}
}

func (s *Service) portsList(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	found, err := s.List(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ports": found}, nil
}
