package sessiongen

import (
	"fmt"
	"time"
)

// The Codex app-server's messages, built with the fields its real
// recordings carry (adapters/codex/testdata) and nothing else. The Codex
// profile writes them to its journal; the test app-server
// (enginetest.CodexServer) sends the few it makes itself (a request
// resolved, an interrupted turn, a thread the terminal starts) through
// the same builders, so honesty_codex_test.go checks them all.

// CodexMsg is one app-server message: a notification, or a server request
// when ID is set.
type CodexMsg = map[string]any

func secsOf(t time.Time) int64 { return t.Unix() }
func msOf(t time.Time) int64   { return t.UnixMilli() }

// CodexThread is the thread object of thread/started and of a
// thread/resume result (appserver-0167.jsonl:1-2, appserver-turn.jsonl:28).
func CodexThread(id, parent, nickname, role, cwd, model, effort string, at time.Time) map[string]any {
	th := map[string]any{"id": id, "parentThreadId": nil, "agentNickname": nil, "agentRole": nil, "name": nil,
		"preview": "", "ephemeral": false, "status": map[string]any{"type": "idle"}, "cwd": cwd,
		"createdAt": secsOf(at), "updatedAt": secsOf(at), "model": model, "reasoningEffort": effort, "turns": []any{}}
	if parent != "" {
		th["parentThreadId"], th["agentNickname"], th["agentRole"] = parent, nickname, role
	}
	return th
}

// CodexThreadStarted is a new thread: the terminal's at launch or after
// /new, a subagent's (appserver-attach.jsonl:1, appserver-0167.jsonl:2).
func CodexThreadStarted(thread map[string]any) CodexMsg {
	return CodexMsg{"method": "thread/started", "params": map[string]any{"thread": thread}}
}

// CodexStatus is thread/status/changed: active, idle, systemError or
// notLoaded (appserver-attach.jsonl).
func CodexStatus(thread, status string) CodexMsg {
	st := map[string]any{"type": status}
	if status == "active" {
		st["activeFlags"] = []any{}
	}
	return CodexMsg{"method": "thread/status/changed", "params": map[string]any{"threadId": thread, "status": st}}
}

// CodexTurn is a turn object as turn/started and turn/completed carry it
// (appserver-attach.jsonl:7, 26).
func CodexTurn(id, status string, started time.Time, ended *time.Time, failure map[string]any) map[string]any {
	t := map[string]any{"id": id, "items": []any{}, "itemsView": "notLoaded", "status": status, "error": nil,
		"startedAt": secsOf(started), "completedAt": nil, "durationMs": nil}
	if ended != nil {
		t["completedAt"] = secsOf(*ended)
		t["durationMs"] = ended.Sub(started).Milliseconds()
	}
	if failure != nil {
		t["error"] = failure
	}
	return t
}

// CodexTurnStarted and CodexTurnCompleted bound a turn.
func CodexTurnStarted(thread string, turn map[string]any) CodexMsg {
	return CodexMsg{"method": "turn/started", "params": map[string]any{"threadId": thread, "turn": turn}}
}

func CodexTurnCompleted(thread string, turn map[string]any) CodexMsg {
	return CodexMsg{"method": "turn/completed", "params": map[string]any{"threadId": thread, "turn": turn}}
}

// CodexItemStarted and CodexItemCompleted carry one thread item.
func CodexItemStarted(thread, turn string, item map[string]any, at time.Time) CodexMsg {
	return CodexMsg{"method": "item/started", "params": map[string]any{"item": item, "threadId": thread, "turnId": turn, "startedAtMs": msOf(at)}}
}

func CodexItemCompleted(thread, turn string, item map[string]any, at time.Time) CodexMsg {
	return CodexMsg{"method": "item/completed", "params": map[string]any{"item": item, "threadId": thread, "turnId": turn, "completedAtMs": msOf(at)}}
}

// CodexUserMessage is the prompt (appserver-turn.jsonl:4).
func CodexUserMessage(id, text string) map[string]any {
	return map[string]any{"type": "userMessage", "id": id, "clientId": nil,
		"content": []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}}}
}

// CodexAgentMessage is a reply: phase commentary or final_answer
// (appserver-turn.jsonl:10-13).
func CodexAgentMessage(id, text, phase string) map[string]any {
	return map[string]any{"type": "agentMessage", "id": id, "text": text, "phase": phase, "memoryCitation": nil}
}

// CodexReasoning is a reasoning item with its summary (appserver-turn.jsonl:6-9).
func CodexReasoning(id string, summary []string) map[string]any {
	s := make([]any, 0, len(summary))
	for _, v := range summary {
		s = append(s, v)
	}
	return map[string]any{"type": "reasoning", "id": id, "summary": s, "content": []any{}}
}

// CodexCommand is a shell command item (appserver-turn.jsonl:14-16,
// appserver-attach.jsonl:23-24). Output and exit code are nil while it
// runs.
func CodexCommand(id, command, cwd, status string, output *string, exit *int, duration *int64) map[string]any {
	it := map[string]any{"type": "commandExecution", "id": id, "command": "/bin/zsh -lc '" + command + "'", "cwd": cwd,
		"processId": nil, "source": "agent", "status": status,
		"commandActions":   []any{map[string]any{"type": "unknown", "command": command}},
		"aggregatedOutput": nil, "exitCode": nil, "durationMs": nil}
	if output != nil {
		it["aggregatedOutput"] = *output
	}
	if exit != nil {
		it["exitCode"] = *exit
	}
	if duration != nil {
		it["durationMs"] = *duration
	}
	return it
}

// CodexFileChange is an apply_patch item (appserver-turn.jsonl:17, 20).
func CodexFileChange(id, status, path, kind, diff string) map[string]any {
	k := map[string]any{"type": kind}
	if kind == "update" {
		k["move_path"] = nil
	}
	return map[string]any{"type": "fileChange", "id": id, "status": status,
		"changes": []any{map[string]any{"path": path, "kind": k, "diff": diff}}}
}

// CodexSpawn is the collab call that starts a subagent thread
// (appserver-turn.jsonl:27).
func CodexSpawn(id, sender, receiver, prompt, status, agentStatus string) map[string]any {
	return map[string]any{"type": "collabAgentToolCall", "id": id, "tool": "spawnAgent", "status": status,
		"senderThreadId": sender, "receiverThreadIds": []any{receiver}, "prompt": prompt, "model": nil, "reasoningEffort": nil,
		"agentsStates": map[string]any{receiver: map[string]any{"status": agentStatus, "message": nil}}}
}

// CodexActivity is a subagent's lifecycle record in its parent's turn:
// kind started or completed (appserver-agent-0167-2.jsonl:1-5).
func CodexActivity(id, kind, child, path string, started time.Time, ended *time.Time) map[string]any {
	it := map[string]any{"type": "subAgentActivity", "model": nil, "reasoningEffort": nil, "id": id, "kind": kind,
		"agentThreadId": child, "agentPath": path, "startedAtMs": msOf(started), "completedAtMs": nil}
	if ended != nil {
		it["completedAtMs"] = msOf(*ended)
	}
	return it
}

// CodexDelta is a streamed piece of a reply, a reasoning summary or a
// command's output (appserver-turn.jsonl:7, 11, 15).
func CodexDelta(method, thread, turn, item, delta string) CodexMsg {
	p := map[string]any{"threadId": thread, "turnId": turn, "itemId": item, "delta": delta}
	if method == "item/reasoning/summaryTextDelta" {
		p["summaryIndex"] = 0
	}
	return CodexMsg{"method": method, "params": p}
}

// CodexPlan is turn/plan/updated (appserver-turn.jsonl:21).
func CodexPlan(thread, turn string, steps []string, done int) CodexMsg {
	plan := make([]any, 0, len(steps))
	for i, s := range steps {
		st := "pending"
		switch {
		case i < done:
			st = "completed"
		case i == done:
			st = "inProgress"
		}
		plan = append(plan, map[string]any{"step": s, "status": st})
	}
	return CodexMsg{"method": "turn/plan/updated", "params": map[string]any{"threadId": thread, "turnId": turn, "explanation": nil, "plan": plan}}
}

// CodexDiff is turn/diff/updated (appserver-turn.jsonl:22).
func CodexDiff(thread, turn, diff string) CodexMsg {
	return CodexMsg{"method": "turn/diff/updated", "params": map[string]any{"threadId": thread, "turnId": turn, "diff": diff}}
}

// CodexTokens is thread/tokenUsage/updated (appserver-turn.jsonl:23).
func CodexTokens(thread, turn string, total, last, window int) CodexMsg {
	use := func(n int) map[string]any {
		return map[string]any{"totalTokens": n, "inputTokens": n * 9 / 10, "cachedInputTokens": 0, "outputTokens": n - n*9/10, "reasoningOutputTokens": 0}
	}
	return CodexMsg{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": thread, "turnId": turn,
		"tokenUsage": map[string]any{"total": use(total), "last": use(last), "modelContextWindow": window}}}
}

// CodexRateLimits is the payload of account/rateLimits/updated and of an
// account/rateLimits/read result (appserver-turn.jsonl:24).
func CodexRateLimits(primary, secondary int, resets time.Time) map[string]any {
	w := func(used, mins int, at time.Time) map[string]any {
		return map[string]any{"usedPercent": used, "windowDurationMins": mins, "resetsAt": secsOf(at)}
	}
	return map[string]any{"rateLimits": map[string]any{"limitId": "codex",
		"primary": w(primary, 300, resets), "secondary": w(secondary, 10080, resets.Add(7*24*time.Hour))}}
}

// CodexLimitsUpdated is account/rateLimits/updated.
func CodexLimitsUpdated(limits map[string]any) CodexMsg {
	return CodexMsg{"method": "account/rateLimits/updated", "params": limits}
}

// CodexError is the error notification of a failed request
// (appserver-attach.jsonl:19).
func CodexError(thread, turn, message, info string, retry bool) CodexMsg {
	return CodexMsg{"method": "error", "params": map[string]any{"error": CodexFailure(message, info),
		"willRetry": retry, "threadId": thread, "turnId": turn}}
}

// CodexFailure is a turn's error object.
func CodexFailure(message, info string) map[string]any {
	return map[string]any{"message": message, "codexErrorInfo": info, "additionalDetails": nil}
}

// CodexSettings is thread/settings/updated (appserver-overrides.jsonl:11).
func CodexSettings(thread, cwd, model, effort, policy string) CodexMsg {
	return CodexMsg{"method": "thread/settings/updated", "params": map[string]any{"threadId": thread,
		"threadSettings": map[string]any{"cwd": cwd, "approvalPolicy": policy, "model": model, "effort": effort}}}
}

// CodexFileApproval is the server request for a file change
// (appserver-0167.jsonl:18).
func CodexFileApproval(id int, thread, turn, item string, at time.Time) CodexMsg {
	return CodexMsg{"id": id, "method": "item/fileChange/requestApproval", "params": map[string]any{"threadId": thread,
		"turnId": turn, "itemId": item, "startedAtMs": msOf(at), "reason": nil, "grantRoot": nil}}
}

// CodexAsk is the server request of the request_user_input tool
// (appserver-turn.jsonl:25).
func CodexAsk(id int, thread, turn, item, qid, header, question string, options ...string) CodexMsg {
	opts := make([]any, 0, len(options))
	for _, o := range options {
		opts = append(opts, map[string]any{"label": o, "description": o})
	}
	q := map[string]any{"id": qid, "header": header, "question": question, "isOther": true, "isSecret": false, "options": opts}
	return CodexMsg{"id": id, "method": "item/tool/requestUserInput", "params": map[string]any{"threadId": thread,
		"turnId": turn, "itemId": item, "questions": []any{q}}}
}

// CodexResolved is serverRequest/resolved: the server says a request was
// answered, by any client (appserver-0167.jsonl:19).
func CodexResolved(thread string, id any) CodexMsg {
	return CodexMsg{"method": "serverRequest/resolved", "params": map[string]any{"threadId": thread, "requestId": id}}
}

// codexUUID makes an id shaped like Codex's UUIDv7 thread and turn ids.
func codexUUID(g *Gen) string {
	return fmt.Sprintf("019a%04x-%s-7%s-8%s-%s", g.Serial(), g.Hex(4), g.Hex(3), g.Hex(3), g.Hex(12))
}
