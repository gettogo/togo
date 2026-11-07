package hookstyle

import (
	"encoding/json"
	"errors"
	"strings"

	claudehook "github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/local"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Reduce turns one hook call into the local.hook params: the summary the
// helper acts on (status, approvals, pushes), read the way Claude Code's
// hooks are, and the redacted, normalized payload for the harness feeds.
// Events the summary has no field for travel as forward-only harness
// events. Errors never quote the payload.
func Reduce(d Dialect, event string, payload []byte, env map[string]string) (wire.HookForward, error) {
	if len(payload) > claudehook.MaxPayload {
		return wire.HookForward{}, errors.New("hookstyle: payload too large")
	}
	canon, norm, err := Normalize(d, event, payload)
	if err != nil {
		return wire.HookForward{}, err
	}
	ev, _, err := claudehook.Map(canon, norm, env)
	switch {
	case errors.Is(err, claudehook.ErrUnsupported):
		var p struct {
			SessionID string `json:"session_id"`
			Cwd       string `json:"cwd"`
		}
		_ = json.Unmarshal(norm, &p)
		ev = wire.HookEvent{SessionID: claudehook.SessionID(env), Event: local.EventHarness, Cwd: p.Cwd}
		ev.Data.AgentSessionID = clip(p.SessionID, 128)
	case err != nil:
		return wire.HookForward{}, err
	}
	ev.Agent = d.Agent
	return claudehook.Forward(ev, canon, norm), nil
}

// Blocks reports whether the hook for a canonical event may wait for an
// answer from the phone: only PermissionRequest, and only for harnesses
// whose hook output can allow or deny.
func Blocks(d Dialect, canon string) bool {
	return d.Answer != "" && canon == PermissionRequest
}

// Decision is what a blocking hook prints for an answer, or nil when the
// answer is neither allow nor deny, so the harness asks in the terminal.
func Decision(d Dialect, answer string) []byte {
	switch d.Answer {
	case AnswerPermissionRequest:
		return claudehook.Decision(answer)
	case AnswerPermissionDecision:
		var out map[string]any
		switch answer {
		case "Allow":
			out = map[string]any{"permissionDecision": "allow"}
		case "Deny", wire.DecisionDeny:
			out = map[string]any{"permissionDecision": "deny", "permissionDecisionReason": "Denied from Togo"}
		default:
			return nil
		}
		b, _ := json.Marshal(out)
		return b
	}
	return nil
}

// clip trims s to n runes and drops control characters.
func clip(s string, n int) string {
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count == n {
			break
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}
