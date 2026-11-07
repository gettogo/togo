package codex

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness/source/rpc"
)

// Answer shapes for the app-server's server requests. The phone answers a
// question item with args {"answers": {question id: [option labels or
// text]}}. Command and file approvals are not answered here: they reach the
// phone through the PermissionRequest hook and are decided there.

var errAnswer = errors.New("codex: answer does not fit the request")

func registerAnswers() {
	rpc.RegisterAnswer("item/tool/requestUserInput", answerUserInput)
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
