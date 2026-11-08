package aider

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The owner's start-up with a model aider has no metadata for: the
// launch line and the warning showed as "✓" steps under "Worked 1s · 2
// steps". The launch line is dropped and the warning is a warning notice.
func TestStartupWarningIsANotice(t *testing.T) {
	h := newHistory(harness.Env{Cwd: t.TempDir(), Vars: map[string]string{}})
	items, _ := h.Map(record(t, []string{
		"# aider chat started at 2026-11-07 23:10:02",
		"",
		"> /Users/jamie/.local/bin/aider  ",
		"> Warning for openai/qwen3.8-27b: Unknown context window size and costs, using sane defaults.  ",
		"> You can skip this check with --no-show-model-warnings  ",
		"> https://aider.chat/docs/llms/warnings.html  ",
		"> Open documentation url for more info? (Y)es/(N)o/(D)on't ask again [Yes]: n  ",
		"",
	}))
	var notices, tools int
	for _, it := range latest(items) {
		switch it.Kind {
		case harness.KindTool:
			tools++
			if it.Tool.Title == "/Users/jamie/.local/bin/aider" {
				t.Fatalf("launch line is a step: %+v", it.Tool)
			}
		case harness.KindNotice:
			notices++
			n := it.Notice
			if n.Level != "warning" || !strings.HasPrefix(n.Text, "Warning for openai/qwen3.8-27b: Unknown context window size and costs, using sane defaults.") {
				t.Fatalf("warning notice = %+v", n)
			}
		}
	}
	if notices != 1 || tools != 0 {
		t.Fatalf("notices = %d, tools = %d; want only the warning (the answered question shows as its Needs you card)", notices, tools)
	}
}
