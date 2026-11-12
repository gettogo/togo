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
// adapter's agent-session memory lasts one call; a session is known only
// from TOGO_SESSION, which Togo sets for the Codex it starts. A Codex run
// in a plain terminal is never attributed to a Togo session.
func codexHook(ctx context.Context, event string, payload []byte, env map[string]string) (wire.HookEvent, string, error) {
	a := &codex.Adapter{Logf: hookLogger(env)}
	ev, err := a.Map(event, bytes.NewReader(payload), env)
	return ev, "", err
}
