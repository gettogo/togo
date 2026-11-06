package session

import "testing"

func TestResumable(t *testing.T) {
	for _, tc := range []struct {
		agent string
		argv  []string
		want  bool
	}{
		{"claude", []string{"claude"}, true},
		{"claude", []string{"claude", "--model", "opus"}, true},
		{"claude", []string{"claude", "fix bug"}, false},
		{"claude", []string{"claude", "--model", "opus", "fix bug"}, false},
		{"claude", []string{"claude", "--flag=x", "prompt"}, false},
		{"claude", []string{"claude", "--flag=x"}, true},
		{"claude", []string{"claude", "--", "text"}, false},
		{"claude", []string{"claude", "--"}, false},
		{"claude", []string{"claude", "-", "x"}, false},
		{"claude", []string{"claude", "--continue"}, false},
		{"codex", []string{"codex"}, true},
		{"codex", []string{"codex", "-m", "o3", "-c", "model=\"o3\""}, true},
		{"codex", []string{"codex", "fix it"}, false},
		{"codex", []string{"codex", "exec", "fix it"}, false},
		{"codex", []string{"codex", "resume", "--last"}, false},
		{"codex", []string{"codex", "-m", "o3", "--", "fix it"}, false},
		// The rule cannot tell a boolean flag from one taking a value:
		// a word after any flag without "=" counts as that flag's value.
		{"codex", []string{"codex", "--full-auto", "word"}, true},
		{"omp", []string{"omp"}, false},
		{"shell", nil, false},
		{"custom", []string{"/bin/agent"}, false},
	} {
		if got := resumable(tc.agent, tc.argv); got != tc.want {
			t.Errorf("resumable(%s, %q) = %v, want %v", tc.agent, tc.argv, got, tc.want)
		}
	}
}
