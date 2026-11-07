package claude

import (
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
)

// usage is an assistant message's token counts.
type usage struct {
	Input         int `json:"input_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
}

// Context windows. Claude Code 2.1.319 gives the 5.x models a 1M window
// (its status line shows "/1000.0k" for Haiku 5.5); older models have 200k
// unless the [1m] alias is used, which the transcript does not record, so
// a prompt larger than 200k means the 1M window.
const (
	window200k = 200_000
	window1M   = 1_000_000
)

// bigWindow matches model ids of generation 5 and later: claude-opus-5-5.
var bigWindow = regexp.MustCompile(`^claude-[a-z]+-([5-9]|\d{2,})(-|$)`)

// contextOps reports how full the context window is from the main chain's
// latest message, so Status shows it without Togo's status line. The
// status line, when it reports, also sends meters and cost; the engine
// keeps those when this partial block arrives.
func contextOps(m message) []harness.Op {
	if m.Usage == nil {
		return nil
	}
	used := m.Usage.Input + m.Usage.CacheCreation + m.Usage.CacheRead
	if used <= 0 {
		return nil
	}
	window := window200k
	if bigWindow.MatchString(m.Model) || strings.Contains(m.Model, "[1m]") || used > window200k {
		window = window1M
	}
	pct := float64(used) * 100 / float64(window)
	if pct > 100 {
		pct = 100
	}
	pct = float64(int(pct*10+0.5)) / 10
	return []harness.Op{{Op: harness.OpStatus, Status: &harness.StatusBlock{ContextPercent: &pct}}}
}
