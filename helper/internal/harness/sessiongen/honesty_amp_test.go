package sessiongen

func init() {
	hookSpecs["amp"] = hookSpec{
		profile:  func() Profile { return &Amp{} },
		fixtures: []string{"hookstyle/testdata/amp.jsonl"},
		reads: map[string][]string{
			"session.start": {"thread"}, "agent.start": {"thread", "message"},
			"tool.result": {"thread", "toolUseID", "tool", "input", "status"}, "agent.end": {"thread", "status", "last"},
		},
	}
}
