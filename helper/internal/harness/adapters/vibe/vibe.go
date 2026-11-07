// Package vibe is the Mistral Vibe harness adapter: its adapter file, the
// vibe.hooks mapper (hooks that speak Vibe's claude_code protocol, read by
// hookstyle) and the vibe.session mapper, which reads the message journal
// Vibe appends at ~/.vibe/logs/session/<dir>/messages.jsonl next to
// meta.json. The journal lines are Vibe's LLMMessage records (role,
// content, reasoning_content, tool_calls, tool_call_id, tool_result); the
// layout was read from Vibe 3.1's source, not from a live session.
package vibe

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

//go:embed vibe.yaml
var adapterFile []byte

// Mapper names, as vibe.yaml uses them.
const (
	MapperHooks   = "vibe.hooks"
	MapperSession = "vibe.session"
)

func init() {
	harness.RegisterAdapterFile("vibe", adapterFile)
	hookstyle.Register(hookstyle.Options{Name: MapperHooks, Transcript: journalFor})
	harness.RegisterMapper(MapperSession, func(env harness.Env) harness.Mapper { return newSession(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }

// maxMeta bounds a meta.json read.
const maxMeta = 256 << 10

// journalFor finds the journal of the newest Vibe session that works in
// the session's folder and changed since the session started.
func journalFor(env harness.Env) string {
	if env.Cwd == "" {
		return ""
	}
	home := source.Expand("~", env.Vars[source.VarHome])
	metas, _ := filepath.Glob(filepath.Join(home, ".vibe", "logs", "session", "*", "meta.json"))
	created, _ := time.Parse(time.RFC3339, env.Vars["created"])
	best, bestAt := "", time.Time{}
	for _, m := range metas {
		st, err := os.Stat(m)
		if err != nil || st.Size() > maxMeta {
			continue
		}
		journal := filepath.Join(filepath.Dir(m), "messages.jsonl")
		jst, err := os.Stat(journal)
		if err != nil || (!created.IsZero() && jst.ModTime().Before(created.Add(-time.Minute))) || !jst.ModTime().After(bestAt) {
			continue
		}
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var meta struct {
			Environment struct {
				WorkingDirectory string `json:"working_directory"`
			} `json:"environment"`
			Origin string `json:"origin_directory"`
		}
		if json.Unmarshal(b, &meta) != nil {
			continue
		}
		if filepath.Clean(meta.Environment.WorkingDirectory) != filepath.Clean(env.Cwd) && filepath.Clean(meta.Origin) != filepath.Clean(env.Cwd) {
			continue
		}
		best, bestAt = journal, jst.ModTime()
	}
	return best
}
