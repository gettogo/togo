package rpc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// On connect the feed reads the account's rate limits once and passes them
// on as the update notification, so the meters show before a turn ends.
func TestWatchReadsLimitsAtConnect(t *testing.T) {
	sock := filepath.Join(socketDir(t), "s.sock")
	f := startFake(t, sock)
	f.handle = func(method string, params json.RawMessage) (any, *Error) {
		if method == methodLimits {
			return map[string]any{"rateLimits": map[string]any{"primary": map[string]any{"usedPercent": 41}}}, nil
		}
		return resumeHandler(method, params)
	}
	r := NewRunner()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &recorder{}
	env := harness.Env{SessionID: "s_l", Endpoint: "unix://" + sock}
	go r.Watch(ctx, feedSpec(t, spec.RPC{Method: MethodResume, Params: map[string]string{"threadId": "thr-1"},
		Notifications: []string{methodLimitsUpdated}}), env, rec.emit)
	waitFor(t, "limits record", func() bool { return rec.has(methodLimitsUpdated) })
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, m := range rec.recs {
		if m.Method == methodLimitsUpdated && !strings.Contains(string(m.Params), `"usedPercent":41`) {
			t.Fatalf("params %s", m.Params)
		}
	}
}
