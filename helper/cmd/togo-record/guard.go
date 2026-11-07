package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// configFiles are the settings an agent could change through itself during
// a scenario (Claude's `/model` saves the default for new sessions, for
// one). Only the recorded agent's own files are watched, so a change the
// user or another tool makes to another agent's settings meanwhile is never
// touched. A changed file is put back byte for byte, its changed version
// kept next to it (".togo-record-changed"), and the fixture notes it.
var configFiles = map[string][]string{
	"claude":   {".claude/settings.json"},
	"codex":    {".codex/config.toml"},
	"gemini":   {".gemini/settings.json"},
	"opencode": {".config/opencode/opencode.json", ".config/opencode/opencode.jsonc"},
	"omp":      {".omp/agent/config.yml"},
	"qwen":     {".qwen/settings.json"},
	"droid":    {".factory/settings.json"},
	"copilot":  {".copilot/config.json"},
	"crush":    {".config/crush/crush.json"},
	"goose":    {".config/goose/config.yaml"},
	"kimi":     {".kimi/config.toml"},
	"vibe":     {".vibe/config.toml"},
}

type configGuard struct {
	saved map[string][]byte
	done  bool
}

func snapshotConfigs(agent string) *configGuard {
	home, _ := os.UserHomeDir()
	g := &configGuard{saved: map[string][]byte{}}
	for _, rel := range configFiles[agent] {
		path := filepath.Join(home, rel)
		if b, err := os.ReadFile(path); err == nil {
			g.saved[path] = b
		}
	}
	return g
}

// restore writes back every file that changed and returns their names
// relative to home. Only the first call does anything.
func (g *configGuard) restore() []string {
	if g.done {
		return nil
	}
	g.done = true
	home, _ := os.UserHomeDir()
	var changed []string
	for path, before := range g.saved {
		now, err := os.ReadFile(path)
		if err == nil && bytes.Equal(now, before) {
			continue
		}
		info, statErr := os.Stat(path)
		mode := os.FileMode(0o600)
		if statErr == nil {
			mode = info.Mode().Perm()
		}
		if err == nil {
			_ = os.WriteFile(path+".togo-record-changed", now, 0o600)
		}
		if werr := os.WriteFile(path, before, mode); werr != nil {
			fmt.Fprintf(os.Stderr, "togo-record: could not restore %s: %v\n", path, werr)
		}
		rel, _ := filepath.Rel(home, path)
		changed = append(changed, "~/"+rel)
		fmt.Fprintf(os.Stderr, "togo-record: ~/%s changed during the scenario; restored it (the changed version is in ~/%s.togo-record-changed)\n", rel, rel)
	}
	return changed
}
