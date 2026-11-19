package sessiongen

func init() {
	hookSpecs["droid"] = hookSpec{
		profile:  func() Profile { return &Droid{} },
		fixtures: []string{"droid/testdata/hooks-real-exec.jsonl", "droid/testdata/hooks-ask.jsonl", "hookstyle/testdata/droid.jsonl"},
		base:     []string{"session_id", "transcript_path", "cwd", "permission_mode", "hook_event_name"},
		reads: map[string][]string{
			"SessionStart": {"session_id", "source", "permission_mode"}, "UserPromptSubmit": {"prompt"},
			"PreToolUse": {"tool_name", "tool_input"}, "PostToolUse": {"tool_name", "tool_response"},
			"Stop": {"transcript_path"}, "Notification": {"message"}, "SessionEnd": {"session_id"},
		},
	}
}
