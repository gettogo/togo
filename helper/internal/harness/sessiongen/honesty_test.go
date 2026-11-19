package sessiongen

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The honesty tests hold each profile to what the agent really writes. A
// record is grouped by its kind; every path the generator writes must be
// one the adapter's mapper reads (readOMP, readClaude) and one that a real
// recorded record of the same kind has; and a read path that every real
// record of a kind has must be written too.
//
// The real records are adapters/<agent>/testdata/*.jsonl. For omp,
// observed.jsonl collects the background-agent, job and exit records that
// adapters/omp/{background,exit,subexit,jobs}_test.go quote from omp
// 18.19 to 18.21 sessions.

// readOMP lists the paths omp's mapper reads (adapters/omp/session.go,
// calls.go, jobs.go). A path ending in ".*" covers everything under it:
// tool arguments, which describeTool reads by tool.
var readOMP = []string{
	"type", "id", "parentId", "timestamp", "title", "parentSession", "model", "method", "tokensBefore", "tokensAfter",
	"customType", "data.kind",
	"details.jobs[].jobId", "details.jobs[].agentId", "details.jobs[].type", "details.jobs[].status", "details.jobs[].label",
	"message.role", "message.content", "message.content[text].text", "message.content[thinking].thinking",
	"message.content[toolCall].id", "message.content[toolCall].name", "message.content[toolCall].arguments.*",
	"message.provider", "message.model", "message.stopReason", "message.errorStatus", "message.errorMessage",
	"message.usage.input", "message.usage.output", "message.usage.cacheRead", "message.usage.cost.total",
	"message.toolCallId", "message.isError", "message.details.exitCode",
	"message.details.async.state", "message.details.async.jobId", "message.details.async.type",
	"message.details.results[].id", "message.details.results[].exitCode", "message.details.results[].aborted",
	"message.details.results[].output",
}

// readClaude lists the paths Claude Code's mapper reads
// (adapters/claude/transcript.go entry, message and block; agents.go and
// tools.go for toolUseResult).
var readClaude = []string{
	"type", "subtype", "uuid", "timestamp", "sessionId", "isSidechain", "agentId", "isMeta", "isCompactSummary",
	"operation", "content", "durationMs", "compactMetadata.trigger", "origin.kind",
	"message.id", "message.role", "message.model", "message.content",
	"message.usage.input_tokens", "message.usage.cache_creation_input_tokens", "message.usage.cache_read_input_tokens",
	"message.content[text].text", "message.content[thinking].thinking",
	"message.content[tool_use].id", "message.content[tool_use].name", "message.content[tool_use].input.*",
	"message.content[tool_result].tool_use_id", "message.content[tool_result].is_error", "message.content[tool_result].content",
	"toolUseResult.agentId", "toolUseResult.status", "toolUseResult.description", "toolUseResult.backgroundTaskId",
}

func ompKind(r map[string]any) string {
	m, _ := r["message"].(map[string]any)
	role, _ := m["role"].(string)
	ct, _ := r["customType"].(string)
	return r["type"].(string) + "/" + role + "/" + ct
}

func claudeKind(r map[string]any) string {
	sub, _ := r["subtype"].(string)
	op, _ := r["operation"].(string)
	return r["type"].(string) + "/" + sub + "/" + op
}

// paths flattens a record to its leaf paths. An array element that is an
// object in a content list is named by its type ("content[text].text").
func paths(v any, at string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			out[at] = true
		}
		for k, e := range x {
			p := k
			if at != "" {
				p = at + "." + k
			}
			paths(e, p, out)
		}
	case []any:
		if len(x) == 0 {
			out[at] = true
		}
		for _, e := range x {
			name := "[]"
			if m, ok := e.(map[string]any); ok && strings.HasSuffix(at, "content") {
				if t, ok := m["type"].(string); ok {
					name = "[" + t + "]"
				}
			}
			paths(e, at+name, out)
		}
	default:
		out[at] = true
	}
}

func reads(table []string, p string) bool {
	if strings.HasSuffix(p, "].type") {
		return true // the block's type, which picks how it is read
	}
	for _, r := range table {
		if r == p || strings.HasSuffix(r, ".*") && strings.HasPrefix(p, strings.TrimSuffix(r, "*")) {
			return true
		}
		// An empty list or object is written as the read path itself.
		if strings.HasPrefix(r, p+"[") || strings.HasPrefix(r, p+".") {
			return true
		}
	}
	return false
}

func readRecords(t *testing.T, files []string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(bytes.NewReader(raw))
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) == nil && m["type"] != nil {
				out = append(out, m)
			}
		}
	}
	return out
}

func checkHonest(t *testing.T, real, gen []map[string]any, kind func(map[string]any) string, table []string) {
	t.Helper()
	realKinds := map[string][]map[string]bool{}
	for _, r := range real {
		ps := map[string]bool{}
		paths(r, "", ps)
		realKinds[kind(r)] = append(realKinds[kind(r)], ps)
	}
	genKinds := map[string]map[string]bool{}
	for _, r := range gen {
		k := kind(r)
		ps := map[string]bool{}
		paths(r, "", ps)
		if genKinds[k] == nil {
			genKinds[k] = map[string]bool{}
		}
		for p := range ps {
			genKinds[k][p] = true
		}
	}
	for k, ps := range genKinds {
		reals, ok := realKinds[k]
		if !ok {
			t.Errorf("kind %s: no real record has it", k)
			continue
		}
		for p := range ps {
			if !reads(table, p) {
				t.Errorf("kind %s: %s is not a field the mapper reads", k, p)
			}
			seen := false
			for _, r := range reals {
				seen = seen || r[p]
			}
			if !seen {
				t.Errorf("kind %s: %s is in no real record of this kind", k, p)
			}
		}
		var required []string
		for p := range reals[0] {
			all := reads(table, p) && !strings.Contains(p, ".arguments.") && !strings.Contains(p, ".input.")
			for _, r := range reals[1:] {
				all = all && r[p]
			}
			if all && !ps[p] {
				required = append(required, p)
			}
		}
		sort.Strings(required)
		for _, p := range required {
			t.Errorf("kind %s: every real record has %s, which the mapper reads, and the generator leaves it out", k, p)
		}
	}
}

// oneOfEach is a short script that writes every record kind a profile
// knows.
func oneOfEach(cwd string) Script {
	return Script{Seed: 7, Start: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Cwd: cwd, Turns: []Turn{
		{Say: 40, Tools: 1, Output: 30, Jobs: 1},
		{Compact: true, Agents: &Batch{Size: 2, Steps: 1, Background: true}, Say: 20},
		{Agents: &Batch{Size: 2, Steps: 1}, Say: 20},
		{Interrupt: true},
		{Error: true, Exit: true},
	}}
}

func TestOMPMatchesRecordedFields(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	l, err := Write(home, &OMP{TTY: "ttys042"}, oneOfEach(cwd))
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.Glob("../adapters/omp/testdata/*.jsonl")
	checkHonest(t, readRecords(t, real), readRecords(t, append(l.Mains, l.Subagents...)), ompKind, readOMP)
}

func TestClaudeMatchesRecordedFields(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	s := oneOfEach(cwd)
	s.Turns = []Turn{
		{Say: 40, Tools: 1, Output: 30, Jobs: 1},
		{Compact: true, Agents: &Batch{Size: 2, Steps: 1, Background: true}, Say: 20},
		{Interrupt: true},
	}
	l, err := Write(home, &Claude{}, s)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.Glob("../adapters/claude/testdata/*.jsonl")
	checkHonest(t, readRecords(t, real), readRecords(t, append(l.Mains, l.Subagents...)), claudeKind, readClaude)
}

// The same script and seed write the same bytes.
func TestDeterministic(t *testing.T) {
	write := func() map[string]string {
		home := filepath.Join(t.TempDir(), "h")
		l, err := Write(home, &OMP{TTY: "ttys042"}, oneOfEach(filepath.Join(home, "proj")))
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, p := range append(append(l.Mains, l.Subagents...), l.Terminal) {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(home, p)
			out[rel] = strings.ReplaceAll(string(b), home, "HOME")
		}
		return out
	}
	a, b := write(), write()
	if len(a) != len(b) {
		t.Fatalf("%d files, then %d", len(a), len(b))
	}
	for p, body := range a {
		if b[p] != body {
			t.Errorf("%s differs between two runs", p)
		}
	}
}

// Subagent files arrive in a seeded order, not the order of the call.
func TestSubagentOrderFollowsSeed(t *testing.T) {
	orders := map[string]bool{}
	for seed := int64(1); seed <= 6; seed++ {
		home := t.TempDir()
		l, err := Write(home, &OMP{}, Script{Seed: seed, Start: time.Now(), Cwd: filepath.Join(home, "p"),
			Turns: []Turn{{Agents: &Batch{Size: 4, Background: true}}}})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, a := range l.Truth.Agents {
			names = append(names, strings.SplitN(a.Task, "x", 2)[0])
		}
		orders[strings.Join(names, ",")] = true
	}
	if len(orders) < 2 {
		t.Fatalf("six seeds gave one arrival order: %v", orders)
	}
}
