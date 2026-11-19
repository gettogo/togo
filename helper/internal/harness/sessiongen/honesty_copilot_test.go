package sessiongen

func init() {
	hookSpecs["copilot"] = hookSpec{
		profile:  func() Profile { return &Copilot{} },
		fixtures: []string{"copilot/testdata/hooks-ask.jsonl", "hookstyle/testdata/copilot.jsonl"},
		quoted: map[string][]string{
			// hookstyle/dialect_more_test.go:34-38, mapper_turns_test.go:157.
			"subagentStart": {"agentName"}, "subagentStop": {"agentName"},
			// hookstyle/failures_test.go:115: sessionEnd with an error.
			"sessionEnd": {"reason", "error"},
		},
		base: []string{"sessionId", "timestamp", "cwd"},
		reads: map[string][]string{
			"sessionStart": {"sessionId", "source"}, "userPromptSubmitted": {"sessionId", "prompt"},
			"preToolUse": {"sessionId", "toolCallId", "toolName", "toolArgs"}, "permissionRequest": {"toolName", "toolInput"},
			"postToolUse": {"toolCallId", "toolName", "toolResult"}, "agentStop": {"sessionId"},
			"sessionEnd": {"reason"}, "errorOccurred": {"error"},
			"subagentStart": {"agentName"}, "subagentStop": {"agentName"},
		},
	}
}
