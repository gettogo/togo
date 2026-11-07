package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// askPayload is the part of a Claude Code PermissionRequest payload for
// AskUserQuestion that the question route reads.
type askPayload struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Questions json.RawMessage `json:"questions"`
	} `json:"tool_input"`
}

// claudeAsk reads an AskUserQuestion PermissionRequest payload: the raw
// questions (echoed back in updatedInput) and their prompts in order.
func claudeAsk(payload []byte) (json.RawMessage, []string, bool) {
	var p askPayload
	if json.Unmarshal(payload, &p) != nil || p.ToolName != "AskUserQuestion" || len(p.ToolInput.Questions) == 0 {
		return nil, nil, false
	}
	var qs []struct {
		Question string `json:"question"`
	}
	if json.Unmarshal(p.ToolInput.Questions, &qs) != nil || len(qs) == 0 || len(qs) > wire.MaxQuestions {
		return nil, nil, false
	}
	prompts := make([]string, len(qs))
	for i, q := range qs {
		if q.Question == "" || len(q.Question) > wire.MaxQuestionPrompt {
			return nil, nil, false
		}
		prompts[i] = q.Question
	}
	return p.ToolInput.Questions, prompts, true
}

// claudeQuestion holds Claude Code's question dialog for the phone: the
// daemon answers once the question item is answered there, and the hook
// allows the tool with the answers as its updatedInput, which is how
// Claude Code takes answers it did not collect itself. The dialog stays
// live at the desk meanwhile; an answer there ends the tool and this hook.
// No answer (timeout, no device, no daemon) prints nothing.
func claudeQuestion(ctx context.Context, sock string, ev wire.HookEvent, questions json.RawMessage, prompts []string, stdout io.Writer, log func(string, ...any)) {
	ctx, cancel := context.WithTimeout(ctx, hookAskSeconds*time.Second+hookAskSlack)
	defer cancel()
	var res wire.QuestionResult
	params := wire.QuestionParams{SessionID: ev.SessionID, Questions: prompts, TimeoutSec: hookAskSeconds}
	if err := localCall(ctx, sock, wire.MethodLocalQuestion, params, &res); err != nil {
		log("hook claude PermissionRequest AskUserQuestion: %v", err)
		return
	}
	if out := questionDecision(questions, prompts, res.Answers); out != nil {
		fmt.Fprintln(stdout, string(out))
	}
}

// questionDecision is the PermissionRequest output that answers every
// question, or nil when an answer is missing.
func questionDecision(questions json.RawMessage, prompts []string, answers map[string]string) []byte {
	if len(answers) == 0 {
		return nil
	}
	for _, p := range prompts {
		if answers[p] == "" {
			return nil
		}
	}
	b, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PermissionRequest",
			"decision": map[string]any{
				"behavior":     "allow",
				"updatedInput": map[string]any{"questions": questions, "answers": answers},
			},
		},
	})
	if err != nil {
		return nil
	}
	return b
}
