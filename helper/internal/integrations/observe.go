package integrations

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/hooks"
)

// Observe wraps local.hook so the service notes when each harness's hook
// (and Claude's status line) last reached the daemon; doctor and the app
// read it to tell a hook feed that never fired from one that works. The
// record is written at most once a minute per harness.
func (s *Service) Observe(home string, next api.Method) api.Method {
	if next == nil {
		return nil
	}
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		res, err := next(ctx, raw)
		if err == nil {
			var p struct {
				Agent string `json:"agent"`
				Hook  string `json:"hook"`
			}
			if json.Unmarshal(raw, &p) == nil && hooks.Known(p.Agent) {
				s.note(home, p.Agent, p.Hook == "statusline")
			}
		}
		return res, err
	}
}

func (s *Service) note(home, agent string, statusLine bool) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	key := agent
	if statusLine {
		key += hooks.SeenStatusLine
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadSeen(home)
	s.seen[key] = now
	if last, ok := s.written[key]; ok && now.Sub(last) < seenEvery {
		return
	}
	s.written[key] = now
	if err := s.seen.Save(home); err != nil && s.Logf != nil {
		s.Logf("integrations: %v", err)
	}
}
