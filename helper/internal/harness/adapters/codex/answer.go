package codex

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness/source/rpc"
)

// Answer shapes for the app-server's server requests. The phone answers a
// question item with args {"answers": {question id: [option labels or
// text]}}; a command or file-change approval is a question whose one
// answer is the decision.

var errAnswer = errors.New("codex: answer does not fit the request")

func registerAnswers() {
	rpc.RegisterAnswer("item/tool/requestUserInput", answerUserInput)
	rpc.RegisterAnswer("item/commandExecution/requestApproval", answerApproval)
	rpc.RegisterAnswer("item/fileChange/requestApproval", answerApproval)
	rpc.RegisterAnswer("item/permissions/requestApproval", answerPermissions)
}

// answerPermissions builds a PermissionsRequestApprovalResponse
// ({"permissions": GrantedPermissionProfile, "scope": "turn"|"session"}):
// accept grants every permission the request asked for, for the turn;
// acceptForSession for the session; decline grants none. Checked live on
// 0.167.0: the model is told "granted" or "denied" accordingly.
func answerPermissions(params json.RawMessage, args map[string]json.RawMessage) (json.RawMessage, error) {
	given, err := answersArg(args)
	if err != nil {
		return nil, err
	}
	vals := given["decision"]
	if len(vals) != 1 {
		return nil, errAnswer
	}
	var p struct {
		Permissions map[string]json.RawMessage `json:"permissions"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, errAnswer
	}
	granted := map[string]json.RawMessage{}
	for k, v := range p.Permissions {
		if len(v) > 0 && string(v) != "null" {
			granted[k] = v
		}
	}
	switch strings.TrimSpace(vals[0]) {
	case "accept":
		return json.Marshal(map[string]any{"permissions": granted, "scope": "turn"})
	case "acceptForSession":
		return json.Marshal(map[string]any{"permissions": granted, "scope": "session"})
	case "decline":
		return json.Marshal(map[string]any{"permissions": map[string]any{}, "scope": "turn"})
	}
	return nil, errAnswer
}

// answerApproval builds {"decision": d} for a command or file-change
// approval from the question's one answer, which is the decision id.
func answerApproval(_ json.RawMessage, args map[string]json.RawMessage) (json.RawMessage, error) {
	given, err := answersArg(args)
	if err != nil {
		return nil, err
	}
	vals := given["decision"]
	if len(vals) != 1 {
		return nil, errAnswer
	}
	d := strings.TrimSpace(vals[0])
	switch d {
	case "accept", "acceptForSession", "decline", "cancel":
	default:
		return nil, errAnswer
	}
	return json.Marshal(map[string]string{"decision": d})
}

func answersArg(args map[string]json.RawMessage) (map[string][]string, error) {
	var out map[string][]string
	raw, ok := args["answers"]
	if !ok {
		return nil, errAnswer
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errAnswer
	}
	return out, nil
}

// answerUserInput builds {"answers": {id: {"answers": [...]}}} for every
// question the request asked; a question left out gets no answer.
func answerUserInput(params json.RawMessage, args map[string]json.RawMessage) (json.RawMessage, error) {
	given, err := answersArg(args)
	if err != nil {
		return nil, err
	}
	var p struct {
		Questions []struct {
			ID string `json:"id"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, errAnswer
	}
	type answer struct {
		Answers []string `json:"answers"`
	}
	out := map[string]answer{}
	for _, q := range p.Questions {
		vals, ok := given[q.ID]
		if !ok {
			continue
		}
		clean := make([]string, 0, len(vals))
		for _, v := range vals {
			if v = strings.TrimSpace(v); v != "" {
				clean = append(clean, v)
			}
		}
		out[q.ID] = answer{Answers: clean}
	}
	if len(out) == 0 {
		return nil, errAnswer
	}
	return json.Marshal(map[string]any{"answers": out})
}
