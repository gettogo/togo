package generic

import (
	"slices"

	"github.com/gettogo/togo/helper/internal/harness/enginetest"
)

// Codex is read through its app-server (enginetest.CodexServer): the
// helper's actions reach it as JSON-RPC calls, not as typed text or keys.

// codexReads are the calls the helper's feed and sources make on their
// own; any other call is an action.
var codexReads = []string{"initialize", "thread/loaded/list", "thread/resume", "account/rateLimits/read",
	"model/list", "thread/list"}

// codexActions are the action calls among calls, as API calls: the
// method, and the params as the body.
func codexActions(calls []enginetest.CodexCall) []enginetest.APICall {
	var out []enginetest.APICall
	for _, c := range calls {
		if !slices.Contains(codexReads, c.Method) {
			out = append(out, enginetest.APICall{Method: "RPC", Path: c.Method, Body: string(c.Params), Status: 200})
		}
	}
	return out
}

// codexInterrupted reports a turn/interrupt call, and the calls made.
func codexInterrupted(s *enginetest.CodexServer) ([]string, bool) {
	var methods []string
	for _, c := range codexActions(s.Calls()) {
		methods = append(methods, c.Path)
	}
	return methods, slices.Contains(methods, "turn/interrupt")
}
