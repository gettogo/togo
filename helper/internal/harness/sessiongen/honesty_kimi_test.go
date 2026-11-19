package sessiongen

import (
	"path/filepath"
	"testing"
	"time"
)

// readKimi lists the paths the Kimi wire mapper reads (adapters/kimi:
// wire.go record and part, loop.go loopEvent, interact.go interaction and
// resolution, errors.go wireError, tasks.go).
var readKimi = []string{
	"type", "time", "protocol_version", "modelAlias", "thinkingEffort", "mode", "turnId", "promptId", "reason",
	"input[].type", "input[].text", "origin.kind", "origin.taskId", "origin.status",
	"id", "kind", "toolCallId", "summary",
	"error.code", "error.name", "error.message", "error.details.statusCode", "error.details.maxSteps",
	"event.type", "event.stepUuid", "event.part.type", "event.part.text", "event.part.think",
	"event.toolCallId", "event.name", "event.args.*", "event.description", "event.finishReason",
	"event.display.kind", "event.display.operation", "event.display.path", "event.display.command",
	"event.result.output", "event.result.isError",
	"request.toolName", "request.action", "request.display.kind", "request.display.plan", "request.display.options[].label",
	"request.display.options[].description", "request.questions[].question", "request.questions[].header",
	"request.questions[].multiSelect", "request.questions[].options[].label", "request.questions[].options[].description",
	"response.answers.*", "response.decision", "response.scope", "response.feedback", "response.selectedLabel",
	"info.taskId", "info.description", "info.command", "info.startedAt",
}

func kimiKind(r map[string]any) string {
	k := r["type"].(string)
	if e, ok := r["event"].(map[string]any); ok {
		k += "/" + e["type"].(string)
	}
	if v, ok := r["kind"].(string); ok && (k == "interaction.request") {
		k += "/" + v
	}
	if o, ok := r["origin"].(map[string]any); ok && k == "turn.steer" {
		k += "/" + o["kind"].(string)
	}
	return k
}

// The Kimi profile writes the records of Kimi's real wire logs
// (adapters/kimi/testdata) and nothing else.
func TestKimiMatchesRecordedFields(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	l, err := Write(home, &Kimi{Extra: map[int]KimiTurn{
		1: {Model: "other/model", Effort: "low", Mode: "yolo", Edits: 1, Todos: []string{"done", "in_progress", "pending"}},
		2: {Plan: true, Approval: &KimiApproval{Command: "make", Decision: "approved", Scope: "session"}},
		3: {Approval: &KimiApproval{Command: "make clean", Decision: "rejected", Feedback: "Use make"}},
		4: {Question: &KimiQuestion{Question: "Which database?", Options: []string{"A", "B"}, Answer: "A"}},
		5: {Question: &KimiQuestion{Question: "Which features?", Options: []string{"A", "B"}, Answer: "A, B", Multi: true}},
	}}, Script{Seed: 7, Start: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Cwd: cwd, Turns: []Turn{
		{Say: 40, Tools: 1, Output: 30, Jobs: 1},
		{Compact: true, Say: 20},
		{Say: 20}, {Say: 20}, {Say: 20},
		{Error: true},
		{New: true, Say: 10},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Mains) != 2 {
		t.Fatalf("%d files, want 2 (one /new)", len(l.Mains))
	}
	real, _ := filepath.Glob("../adapters/kimi/testdata/wire-*.jsonl")
	checkHonest(t, readRecords(t, real), readRecords(t, l.Mains), kimiKind, readKimi)
}

func TestKimiRefusesWhatItCannotShow(t *testing.T) {
	home := t.TempDir()
	for _, turn := range []Turn{{Agents: &Batch{Size: 1}}, {Interrupt: true}, {Exit: true}} {
		_, err := Write(home, &Kimi{}, Script{Seed: 1, Start: time.Now(), Cwd: filepath.Join(home, "p"), Turns: []Turn{turn}})
		if err == nil {
			t.Errorf("%+v written without a real recording", turn)
		}
	}
}
