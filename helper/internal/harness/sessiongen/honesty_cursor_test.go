package sessiongen

func init() {
	hookSpecs["cursor"] = hookSpec{
		profile:  func() Profile { return &Cursor{} },
		fixtures: []string{"cursor/testdata/hooks-real-shell.jsonl", "hookstyle/testdata/cursor.jsonl"},
		quoted: map[string][]string{
			// hookstyle/mapper_turns_test.go:124.
			"subagentStart": {"conversation_id", "session_id", "subagent_id", "subagent_type"},
			"subagentStop":  {"conversation_id", "session_id", "subagent_id", "subagent_type"},
		},
		base: []string{"conversation_id", "generation_id", "model", "session_id", "hook_event_name", "cursor_version", "workspace_roots", "transcript_path"},
		reads: map[string][]string{
			"sessionStart": {"session_id"}, "beforeSubmitPrompt": {"prompt", "model"},
			"preToolUse": {"tool_name", "tool_input", "tool_use_id"}, "postToolUse": {"tool_use_id", "tool_name", "tool_output"},
			"postToolUseFailure": {"tool_use_id", "error_message"}, "afterAgentResponse": {"text"}, "stop": {"status"},
			"subagentStart": {"subagent_id", "subagent_type"}, "subagentStop": {"subagent_id", "subagent_type"},
		},
	}
}
