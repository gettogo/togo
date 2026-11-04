package main

import (
	"bytes"
	"context"

	"github.com/gettogo/togo/helper/internal/adapters/codex"
	"github.com/gettogo/togo/helper/internal/wire"
)

func init() {
	hookAdapters["codex"] = codexHook
}

// codexHook runs the Codex adapter. Each hook is its own process, so the
// adapter's agent-session memory lasts one call; sessions are resolved from
// TOGO_SESSION or a unique cwd match among the daemon's codex sessions.
func codexHook(ctx context.Context, event string, payload []byte, env map[string]string) (wire.HookEvent, string, error) {
	a := &codex.Adapter{
		Sessions: func(ctx context.Context) ([]codex.Candidate, error) {
			sock, err := tapSocketPath(env)
			if err != nil {
				return nil, err
			}
			var res struct {
				Sessions []struct {
					ID      string `json:"id"`
					Agent   string `json:"agent"`
					Cwd     string `json:"cwd"`
					Managed bool   `json:"managed"`
				} `json:"sessions"`
			}
			if err := localCall(ctx, sock, "session.list", struct{}{}, &res); err != nil {
				return nil, err
			}
			var out []codex.Candidate
			for _, s := range res.Sessions {
				if s.Managed {
					out = append(out, codex.Candidate{ID: s.ID, Agent: s.Agent, Cwd: s.Cwd})
				}
			}
			return out, nil
		},
		Logf: hookLogger(env),
	}
	ev, err := a.Map(event, bytes.NewReader(payload), env)
	return ev, "", err
}
