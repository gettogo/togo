package omp

import "testing"

func TestSwitchedModelFromScreen(t *testing.T) {
	for name, tc := range map[string]struct{ screen, want string }{
		"wide": {"\n Session-only model: anthropic/claude-opus-5-5. Use ⌥M or /model for roles.\n\n π > ◑ Opus 5.5 > x\n╰─\n", "anthropic/claude-opus-5-5"},
		// The pane at the phone's 47 columns (recorded live).
		"narrow":         {" Session-only model:\n anthropic/claude-opus-5-5. Use ⌥M or /model\n for roles.\n π > ◑ Opus 5.5 > 🗑 …pad/omp3 > (sub) ▶─2%─┃1M─\n", "anthropic/claude-opus-5-5"},
		"split selector": {" Session-only model: amazon-bedrock/anthropic.cl\n aude-3-5-haiku-20241022-v1:0. Use ⌥M or\n /model for roles.\n", "amazon-bedrock/anthropic.claude-3-5-haiku-20241022-v1:0"},
		"last wins":      {" Session-only model: anthropic/claude-haiku-4-5. Use ⌥M or /model for roles.\n Session-only model: llama.cpp/qwen3.8-27b. Use ⌥M or /model for roles.\n", "llama.cpp/qwen3.8-27b"},
		"none":           {" π > ◑ Qwen3.8 27B (5090) > x\n╰─\n", ""},
		"chat mention":   {" the line says Session-only model: is shown\n", ""},
	} {
		if got := SwitchedModel(tc.screen); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}
