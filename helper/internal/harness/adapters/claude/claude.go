// Package claude is the Claude Code harness adapter: its adapter file and
// the mappers that turn Claude Code's hook payloads, status line payloads
// and transcript JSONL into conversation items and mission control ops.
//
// The transcript format is internal to Claude Code, so the transcript
// mapper reads it defensively: unknown entry types and fields are skipped,
// and the hooks and status line keep the session usable when it changes.
package claude

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

//go:embed claude.yaml
var adapterFile []byte

// Mapper names, as claude.yaml uses them.
const (
	MapperHooks      = "claude.hooks"
	MapperTranscript = "claude.transcript"
	MapperStatusline = "claude.statusline"
)

func init() {
	harness.RegisterAdapterFile("claude", adapterFile)
	harness.RegisterMapper(MapperHooks, func(env harness.Env) harness.Mapper { return newHooks(env) })
	harness.RegisterMapper(MapperTranscript, func(env harness.Env) harness.Mapper { return newTranscript(env) })
	harness.RegisterMapper(MapperStatusline, func(env harness.Env) harness.Mapper { return newStatusline(env) })
}

// Alias reduces a model id or name ("claude-opus-4-6[1m]", "Opus 4.6 (1M
// context)") to the alias /model takes ("opus[1m]").
func Alias(model string) string {
	m := strings.ToLower(model)
	base := ""
	for _, fam := range []string{"opus", "sonnet", "haiku", "fable"} {
		if strings.Contains(m, fam) {
			base = fam
			break
		}
	}
	if base == "" {
		return model
	}
	if strings.Contains(m, "[1m]") || strings.Contains(m, "1m context") || strings.Contains(m, "-1m") {
		return base + "[1m]"
	}
	return base
}

// stamp parses an entry timestamp; a missing one is now.
func stamp(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	return time.Now().UTC()
}

// rel shows a path relative to the session folder when it is inside it.
func rel(cwd, p string) string {
	if p == "" || cwd == "" || !filepath.IsAbs(p) {
		return p
	}
	if r, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

// str reads a JSON string field leniently.
func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

func decodeObj(raw json.RawMessage) map[string]any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// firstLine is the first non-empty line of s.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// plain strips terminal escapes from command output.
func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// transcriptFor finds the transcript of the session folder written since
// the session started, for when no SessionStart hook arrived. Every Claude
// Code session in the folder writes there, Togo's or not, so it guesses
// only when exactly one transcript is that recent: never a neighbour's.
// transcriptSkew allows for "created" being cut to whole seconds. Claude
// writes a transcript only once a prompt is sent, after the session
// started, so a file last written before that belongs to an earlier
// session in the folder.
const transcriptSkew = 2 * time.Second

func transcriptFor(env harness.Env) string {
	key := env.Vars["cwd_key"]
	created, _ := time.Parse(time.RFC3339, env.Vars["created"])
	if key == "" || created.IsZero() {
		return ""
	}
	h := env.Vars[source.VarHome]
	dir := filepath.Join(source.Expand("~", h), ".claude", "projects", key)
	p, err := source.Sole(filepath.Join(dir, "*.jsonl"), created.Add(-transcriptSkew), env)
	if err != nil {
		return ""
	}
	// The session before this one in the folder can write its transcript
	// once more as it is killed, after this one started: a transcript whose
	// first record is older than this session belongs to that one.
	if first, ok := firstStamp(p); ok && first.Before(created.Add(-transcriptSkew)) {
		return ""
	}
	return p
}

// firstStamp reads the first timestamp among a transcript's first lines.
func firstStamp(p string) (time.Time, bool) {
	f, err := os.Open(p)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 1<<20))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for i := 0; i < 20 && sc.Scan(); i++ {
		var e struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}
