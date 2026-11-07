// Package qwen is the Qwen Code harness adapter: its adapter file, the
// qwen.hooks mapper (Claude-shaped hook payloads, read by hookstyle) and
// the qwen.transcript mapper, which reads the chat log Qwen Code appends
// at ~/.qwen/projects/<cwd key>/chats/<session>.jsonl.
//
// The chat log format is internal to Qwen Code. It was read from real
// sessions of 0.21 and 0.22; unknown records are skipped.
package qwen

import (
	_ "embed"
	"path/filepath"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

//go:embed qwen.yaml
var adapterFile []byte

// Mapper names, as qwen.yaml uses them.
const (
	MapperHooks      = "qwen.hooks"
	MapperTranscript = "qwen.transcript"
)

func init() {
	harness.RegisterAdapterFile("qwen", adapterFile)
	hookstyle.Register(hookstyle.Options{Name: MapperHooks, Transcript: transcriptFor, Screen: ReadMode})
	harness.RegisterMapper(MapperTranscript, func(env harness.Env) harness.Mapper { return newTranscript(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }

// transcriptFor finds the chat log of the session folder written since the
// session started, for when no hook named it. Other Qwen Code sessions in
// the folder write there too, so it guesses only when one log is that
// recent.
func transcriptFor(env harness.Env) string {
	key := env.Vars["cwd_key"]
	created, _ := time.Parse(time.RFC3339, env.Vars["created"])
	if key == "" || created.IsZero() {
		return ""
	}
	dir := filepath.Join(source.Expand("~", env.Vars[source.VarHome]), ".qwen", "projects", key, "chats")
	p, err := source.Sole(filepath.Join(dir, "*.jsonl"), created.Add(-time.Minute), env)
	if err != nil {
		return ""
	}
	return p
}

// Mode lines Qwen Code draws in its footer (English locale), by the
// permission_mode its hooks report.
var modeLines = []struct{ text, mode string }{
	{"plan mode", "plan"},
	{"auto-accept edits", "auto_edit"},
	{"yolo mode", "yolo"},
	{"auto mode", "auto"},
}

// ReadMode reads the approval mode from the pane's last lines. The default
// mode draws no line, so it is never reported: a cycle from it falls back.
func ReadMode(text string) map[string]string {
	lines := strings.Split(strings.TrimRight(text, "\n "), "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	for _, l := range lines {
		low := strings.ToLower(l)
		if !strings.Contains(low, "shift + tab") && !strings.Contains(low, "tab to cycle") {
			continue
		}
		for _, m := range modeLines {
			if strings.Contains(low, m.text) {
				return map[string]string{"mode": m.mode}
			}
		}
	}
	return nil
}
