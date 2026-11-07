package engine

import (
	"context"
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// runCall sends an http or rpc route through that kind's runner, on the
// session's helper-assigned endpoint.
func (e *Engine) runCall(ctx context.Context, s *sess, act spec.Action, kind string, block any, data map[string]any) (harness.ActionResult, error) {
	r, ok := harness.LookupRunner(kind)
	if !ok || s.env.Endpoint == "" && s.env.Vars["port"] == "" && s.env.Vars["socket"] == "" {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	raw, err := json.Marshal(block)
	if err != nil {
		return harness.ActionResult{}, err
	}
	rendered, err := RenderJSON(raw, data)
	if err != nil {
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The request could not be built"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, maxTimeout)
	defer cancel()
	if _, err := r.Fetch(ctx, harness.SourceSpec{Name: "action", Kind: kind, Raw: rendered}, s.env); err != nil {
		e.cfg.Logf("harness: %s %s action (%s): %v", s.ad.Adapter.Adapter, kind, act.Stability, err)
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The harness refused the change"}, nil
	}
	e.verify(ctx, s, act.Verify, data)
	return harness.ActionResult{Result: harness.ResultApplied}, nil
}

// rpcAnswerer is the rpc runner's way to answer a server request the
// harness sent (a Codex approval or question), named by target.
type rpcAnswerer interface {
	Answer(ctx context.Context, env harness.Env, target string, args map[string]json.RawMessage) error
}

func (e *Engine) rpcReply(ctx context.Context, s *sess, args map[string]any) (harness.ActionResult, error) {
	r, ok := harness.LookupRunner("rpc")
	ans, isAnswerer := r.(rpcAnswerer)
	if !ok || !isAnswerer {
		return harness.ActionResult{Result: harness.ResultFallback}, nil
	}
	target, _ := args["target"].(string)
	raw := map[string]json.RawMessage{}
	for k, v := range args {
		if k == "target" {
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return harness.ActionResult{}, invalidArgs("args must be JSON")
		}
		raw[k] = b
	}
	if err := ans.Answer(ctx, s.env, target, raw); err != nil {
		e.cfg.Logf("harness: %s answer: %v", s.ad.Adapter.Adapter, err)
		return harness.ActionResult{Result: harness.ResultFailed, Message: "The answer was not accepted"}, nil
	}
	return harness.ActionResult{Result: harness.ResultApplied}, nil
}
