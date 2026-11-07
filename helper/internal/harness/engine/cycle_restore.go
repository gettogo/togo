package engine

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// restoreMode ends a mode cycle that missed its target: it presses on
// until the mode the session started in comes back, so a failed change
// never leaves the session in a mode nobody picked (such as auto), and
// says where it ended.
func (e *Engine) restoreMode(ctx context.Context, s *sess, k *spec.Keys, v *spec.Verify, orig, cur string, within time.Duration, why string) harness.ActionResult {
	for press := 1; cur != orig && press <= maxCycle; press++ {
		if err := e.cfg.Typist.Keys(ctx, s.id, k.Keys...); err != nil {
			break
		}
		next, _, done := e.awaitChange(ctx, s, v, cur, within, press)
		if done {
			break
		}
		cur = next
	}
	if cur == orig {
		return harness.ActionResult{Result: harness.ResultFailed, Message: why + "; back in " + s.modeLabel(orig)}
	}
	if cur == "" {
		return harness.ActionResult{Result: harness.ResultFailed, Message: why + "; check the mode in the terminal"}
	}
	return harness.ActionResult{Result: harness.ResultFailed, Message: why + "; now in " + s.modeLabel(cur)}
}

func (s *sess) modeLabel(id string) string {
	if s.ad.Modes != nil {
		for _, c := range s.ad.Modes.Choices {
			if c.ID == id {
				return c.Label
			}
		}
	}
	return id
}
