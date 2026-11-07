// Package itemtext holds the small text and time helpers the file- and
// hook-based adapters share when they build conversation items.
package itemtext

import (
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Truncate cuts s to n runes, ending with an ellipsis when cut.
func Truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// Title is the first line of s, cut to the title limit.
func Title(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return Truncate(s, harness.MaxTitle)
}

// Tail keeps the last n bytes of s on a rune boundary.
func Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// Clip bounds item text to harness.MaxItemText bytes.
func Clip(s string) string {
	if len(s) <= harness.MaxItemText {
		return s
	}
	s = s[:harness.MaxItemText]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Rel makes p relative to root when it lies under it.
func Rel(root, p string) string {
	if root == "" || !filepath.IsAbs(p) {
		return p
	}
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

// Millis converts Unix milliseconds; zero or less is the zero time.
func Millis(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

// Or returns t, or at when t is zero.
func Or(t, at time.Time) time.Time {
	if t.IsZero() {
		return at
	}
	return t
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }

// Category maps common tool names onto the harness tool categories.
func Category(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "__") || strings.HasPrefix(n, "mcp"):
		return harness.ToolMCP
	case n == "bash" || n == "shell" || strings.Contains(n, "command") || n == "launch-process" || n == "exec":
		return harness.ToolShell
	case n == "read" || strings.HasPrefix(n, "read") || n == "view" || n == "ls":
		return harness.ToolRead
	case n == "edit" || strings.Contains(n, "edit") || strings.Contains(n, "replace") || n == "apply_patch":
		return harness.ToolEdit
	case n == "write" || strings.Contains(n, "write") || strings.Contains(n, "save-file") || n == "create_file":
		return harness.ToolWrite
	case n == "grep" || n == "glob" || n == "find" || strings.Contains(n, "search") && !strings.Contains(n, "web"):
		return harness.ToolSearch
	case strings.Contains(n, "web") || strings.Contains(n, "fetch") || strings.Contains(n, "url"):
		return harness.ToolWeb
	case n == "agent" || n == "task" || strings.Contains(n, "subagent") || strings.Contains(n, "swarm"):
		return harness.ToolAgent
	}
	return harness.ToolOther
}
