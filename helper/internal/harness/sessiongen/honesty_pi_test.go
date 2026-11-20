package sessiongen

import (
	"path/filepath"
	"testing"
	"time"
)

// readPi lists the paths the pisession mapper reads (adapters/pisession/
// pisession.go entry; message.go message and block). Fields the mapper
// does not read (version, usage, api, the inner message timestamps) are
// not listed, so the generator may not write them.
var readPi = []string{
	"type", "id", "parentId", "timestamp", "cwd", "provider", "modelId", "thinkingLevel", "tokensBefore", "name",
	"message.role", "message.content", "message.content[text].text", "message.content[thinking].thinking",
	"message.content[toolCall].id", "message.content[toolCall].name", "message.content[toolCall].arguments.*",
	"message.provider", "message.model", "message.stopReason", "message.errorMessage",
	"message.toolCallId", "message.toolName", "message.isError",
	"message.command", "message.output", "message.exitCode", "message.cancelled",
}

func piKind(r map[string]any) string {
	m, _ := r["message"].(map[string]any)
	role, _ := m["role"].(string)
	return r["type"].(string) + "/" + role
}

// The Pi profile writes the records of Pi's real sessions
// (adapters/pisession/testdata) and nothing else.
func TestPiMatchesRecordedFields(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	l, err := Write(home, &Pi{Extra: map[int]PiTurn{
		1: {Edits: 1, Think: true, Title: "A name"},
		2: {Model: "openai/gpt-5", Effort: "high", Bash: "git status"},
	}}, Script{Seed: 7, Start: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Cwd: cwd, Turns: []Turn{
		{Say: 40, Tools: 1, Output: 30},
		{Say: 20, Compact: true},
		{Interrupt: true},
		{Error: true},
		{New: true, Say: 10},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Mains) != 2 {
		t.Fatalf("%d files, want 2 (one /new)", len(l.Mains))
	}
	real, _ := filepath.Glob("../adapters/pisession/testdata/*.jsonl")
	checkHonest(t, readRecords(t, real), readRecords(t, l.Mains), piKind, readPi)
}

func TestPiRefusesWhatPiLacks(t *testing.T) {
	home := t.TempDir()
	for _, turn := range []Turn{{Agents: &Batch{Size: 1}}, {Jobs: 1}, {Exit: true}} {
		_, err := Write(home, &Pi{}, Script{Seed: 1, Start: time.Now(), Cwd: filepath.Join(home, "p"), Turns: []Turn{turn}})
		if err == nil {
			t.Errorf("%+v: written, but Pi has no such thing", turn)
		}
	}
}
