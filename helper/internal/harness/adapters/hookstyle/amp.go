package hookstyle

import "encoding/json"

// ampPrep reads the events Togo's Amp plugin forwards (Amp's plugin API,
// `amp plugins show-docs` on 0.0.1794): session.start {thread},
// agent.start {thread, message, id}, tool.result {thread, toolUseID, tool,
// input, status, error?, output?} and agent.end {thread, message, id,
// status, last}, where the plugin puts the turn's last assistant text in
// last. A tool.result is the whole call: the plugin does not handle
// tool.call, whose handler must answer allow or reject.
func ampPrep(canon string, obj map[string]json.RawMessage) string {
	var th struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(obj["thread"], &th) == nil && th.ID != "" {
		obj["session_id"], _ = json.Marshal(th.ID)
	}
	delete(obj, "thread")
	var status string
	_ = json.Unmarshal(obj["status"], &status)
	switch canon {
	case UserPromptSubmit:
		move(obj, "message", "prompt")
	case PostToolUse:
		move(obj, "toolUseID", "tool_use_id")
		move(obj, "tool", "tool_name")
		move(obj, "input", "tool_input")
		move(obj, "output", "tool_response")
		obj["tool_once"] = json.RawMessage("true")
		if status == "error" || status == "cancelled" {
			if _, ok := obj["tool_response"]; !ok {
				if e, ok := obj["error"]; ok {
					obj["tool_response"] = e
				}
			}
			return PostToolUseFailure
		}
	case Stop:
		move(obj, "last", "last_assistant_message")
		switch status {
		case "error":
			return StopFailure
		case "cancelled":
			obj["stop_interrupted"] = json.RawMessage("true")
		}
	}
	return canon
}

func move(obj map[string]json.RawMessage, from, to string) {
	if v, ok := obj[from]; ok {
		delete(obj, from)
		if _, has := obj[to]; !has {
			obj[to] = v
		}
	}
}
