package hookstyle

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Failed turns in the hook dialects. Only some agents say in a hook that a
// turn failed, and fewer say why:
//
//   - Droid 1.5's Stop is the same whether the turn worked or not; the
//     reason is in its session transcript (transcript_path) as
//     {"type":"agent_turn_outcome","reason":"model_provider_unreachable"},
//     after a user_only message with Droid's own words. An abort fires no
//     Stop but Notification "Agent stopped by user and is waiting for input".
//   - Copilot 1.0's errorOccurred carries {error, errorContext,
//     recoverable}; a turn whose model call failed for good may never get
//     agentStop. sessionEnd carries reason complete|error|abort|timeout|
//     user_exit and error.
//   - Cursor's stop and Amp's agent.end carry status "error" but no text.
//
// The preps below put what there is into error_details and error_reason
// (a harness.Err* reason) so StopFailure can say it.

// droidOutcomes maps Droid's agent_turn_outcome reasons (its TurnOutcome
// enum) onto how the turn ends: a harness.Err* reason for a failure,
// "interrupted", or "" for a turn that ended normally.
var droidOutcomes = map[string]string{
	"completed": "", "spec_handoff": "",
	"cancelled": "interrupted", "permission_rejected": "interrupted",
	"model_authentication_failed": harness.ErrAuth,
	"model_usage_exhausted":       harness.ErrRateLimit,
	"model_rate_limited":          harness.ErrRateLimit,
	"model_provider_unreachable":  harness.ErrProviderUnreachable,
	"model_provider_unavailable":  harness.ErrProviderUnreachable,
	"process_exit":                harness.ErrCrashed,
	// error, model_request_rejected, prompt_rejected, no_approver_available
	// and the structured-output ones: the words decide.
}

// droidTail is how much of a transcript's end droidOutcome reads.
const droidTail = 256 << 10

func droidPrep(canon string, obj map[string]json.RawMessage) string {
	switch canon {
	case Notification:
		var msg string
		_ = json.Unmarshal(obj["message"], &msg)
		if strings.Contains(strings.ToLower(msg), "stopped by user") {
			obj["stop_interrupted"] = json.RawMessage("true")
			return Stop
		}
	case Stop:
		var path string
		_ = json.Unmarshal(obj["transcript_path"], &path)
		reason, words, ok := droidOutcome(path)
		if !ok {
			return canon
		}
		end, known := droidOutcomes[reason]
		switch {
		case known && end == "":
			return canon
		case end == "interrupted":
			obj["stop_interrupted"] = json.RawMessage("true")
			return canon
		}
		obj["error_details"], _ = json.Marshal(words)
		if end != "" {
			obj["error_reason"], _ = json.Marshal(end)
		}
		return StopFailure
	}
	return canon
}

// droidOutcome reads the last agent_turn_outcome of a Droid transcript and
// the last user_only message before it (Droid's words for a failure).
func droidOutcome(path string) (reason, words string, ok bool) {
	// Only a Droid session file: the path comes from the payload.
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || !strings.HasSuffix(path, ".jsonl") ||
		!strings.Contains(path, string(filepath.Separator)+".factory"+string(filepath.Separator)+"sessions"+string(filepath.Separator)) {
		return "", "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > droidTail {
		f.Seek(st.Size()-droidTail, io.SeekStart)
	}
	b, _ := io.ReadAll(io.LimitReader(f, droidTail))
	return DroidOutcome(b)
}

// DroidOutcome finds the last agent_turn_outcome in transcript lines and
// the last user_only message before it.
func DroidOutcome(b []byte) (reason, words string, ok bool) {
	lines := bytes.Split(b, []byte("\n"))
	at := -1
	for i := len(lines) - 1; i >= 0; i-- {
		var r struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		}
		if !bytes.Contains(lines[i], []byte(`"agent_turn_outcome"`)) || json.Unmarshal(lines[i], &r) != nil || r.Type != "agent_turn_outcome" {
			continue
		}
		reason, at, ok = r.Reason, i, true
		break
	}
	for i := at - 1; i >= 0 && ok; i-- {
		var r struct {
			Type    string `json:"type"`
			Message struct {
				Role       string `json:"role"`
				Visibility string `json:"visibility"`
				Content    []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[i], &r) != nil || r.Type != "message" {
			continue
		}
		if r.Message.Visibility != "user_only" {
			break // the turn's prompt or a reply: no words of Droid's own
		}
		for _, c := range r.Message.Content {
			if c.Type == "text" && c.Text != "" {
				return reason, c.Text, true
			}
		}
	}
	return reason, "", ok
}

// copilotPrep reads Copilot's sessionEnd and errorOccurred.
func copilotPrep(canon string, obj map[string]json.RawMessage) string {
	switch canon {
	case SessionEnd:
		var reason string
		_ = json.Unmarshal(obj["reason"], &reason)
		switch reason {
		case "error", "timeout":
			if reason == "timeout" {
				obj["error_reason"], _ = json.Marshal(harness.ErrProviderUnreachable)
			}
			errorText(obj)
			return StopFailure
		case "abort":
			obj["stop_interrupted"] = json.RawMessage("true")
		}
	case ErrorOccurred:
		var recoverable *bool
		var where string
		_ = json.Unmarshal(obj["recoverable"], &recoverable)
		_ = json.Unmarshal(obj["errorContext"], &where)
		errorText(obj)
		if recoverable != nil && !*recoverable && where != "tool_execution" {
			// The session cannot go on with this turn: end it failed.
			return StopFailure
		}
	}
	return canon
}

// errorText moves {error: {message}} or {error: "text"} into error_details.
func errorText(obj map[string]json.RawMessage) {
	var e struct {
		Message string `json:"message"`
	}
	var str string
	switch {
	case json.Unmarshal(obj["error"], &e) == nil && e.Message != "":
		obj["error_details"], _ = json.Marshal(e.Message)
	case json.Unmarshal(obj["error"], &str) == nil && str != "":
		obj["error_details"], _ = json.Marshal(str)
	}
	delete(obj, "error")
}
