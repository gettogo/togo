package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// pickerItem mirrors harness.PickerItem.
type pickerItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Group    string `json:"group"`
	Disabled bool   `json:"disabled"`
}

// cheapRank scores a model by how cheap its name says it is; lower is
// cheaper. Unknown names rank in the middle, above the flagship tiers.
func cheapRank(it pickerItem) int {
	s := strings.ToLower(it.ID + " " + it.Title + " " + it.Group)
	ranks := []struct {
		words []string
		rank  int
	}{
		{[]string{"haiku", "nano", "flash-lite", "flash lite", "lite"}, 0},
		{[]string{"mini", "flash", "small", "tiny", "free", "turbo"}, 1},
		{[]string{"fast", "instant", "air"}, 2},
		{[]string{"local", "5090", "llama.cpp", "ollama", "lmstudio", ":free"}, 3}, // free, but often busy or offline
		{[]string{"sonnet", "medium"}, 4},
		{[]string{"opus", "max", "ultra", "large", "pro", "1m", "preview", "thinking", "high"}, 8},
	}
	best := 5
	for _, r := range ranks {
		for _, w := range r.words {
			if strings.Contains(s, w) {
				if r.rank > 5 {
					return r.rank // a flagship word beats a cheap one ("max-mini" is not cheap)
				}
				if r.rank < best {
					best = r.rank
				}
			}
		}
	}
	// Cloud-account routes (Bedrock, Vertex, Azure) need credentials a
	// developer machine rarely has; prefer the vendor's own API.
	if strings.Contains(s, "bedrock") || strings.Contains(s, "vertex") || strings.Contains(s, "azure") {
		best += 3
	}
	return best
}

// cheapest returns the enabled models in cheapness order, distinct by
// normalized name. Items in a "not set up" group are left out.
func cheapest(items []pickerItem) []pickerItem {
	var out []pickerItem
	seen := map[string]bool{}
	for _, it := range items {
		g := strings.ToLower(it.Group + " " + it.Subtitle)
		if it.Disabled || it.ID == "" || strings.Contains(g, "not set up") || strings.Contains(g, "sign in") {
			continue
		}
		n := normModel(it.ID)
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return cheapRank(out[i]) < cheapRank(out[j]) })
	return out
}

var dateSuffix = regexp.MustCompile(`-20\d{6}$`)

// normModel reduces a model id to a comparable name: lower case, no
// provider prefix, no variant suffix, dots as dashes, no release date.
func normModel(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "["); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return dateSuffix.ReplaceAllString(s, "")
}

// sameModel reports whether a model the harness recorded is the one the
// suite picked: equal names, or one an alias inside the other ("haiku"
// and "claude-haiku-4-5").
func sameModel(picked, recorded string) bool {
	a, b := normModel(picked), normModel(recorded)
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.Contains(b, a) || strings.Contains(a, b)
}

// truthRoots are where each harness keeps its own session records, which
// name the model each reply came from.
var truthRoots = map[string][]string{
	"claude":  {"~/.claude/projects"},
	"codex":   {"~/.codex/sessions"},
	"omp":     {"~/.omp/agent/sessions"},
	"pi":      {"~/.pi/agent/sessions"},
	"gemini":  {"~/.gemini/tmp"},
	"copilot": {"~/.copilot/session-state"},
	"kimi":    {"~/.kimi-code/sessions", "~/.kimi/sessions"},
	"droid":   {"~/.factory/sessions"},
	"vibe":    {"~/.vibe/logs"},
	"qwen":    {"~/.qwen/projects"},
	"auggie":  {"~/.augment/sessions"},
	"cline":   {"~/.cline/data"},
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// truthFiles returns the harness's session files written since since that
// mention the session's folder (marker), keyed by path with their size.
func truthFiles(harness, marker string, since time.Time) map[string]int64 {
	out := map[string]int64{}
	for _, root := range truthRoots[harness] {
		root = expand(root)
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.Count(strings.TrimPrefix(p, root), string(os.PathSeparator)) > 5 || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(p)
			if ext != ".jsonl" && ext != ".json" {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.ModTime().Before(since) || info.Size() > 64<<20 {
				return nil
			}
			if strings.Contains(p, marker) || fileContains(p, marker) {
				out[p] = info.Size()
			}
			return nil
		})
	}
	return out
}

func fileContains(p, s string) bool {
	b, err := os.ReadFile(p)
	return err == nil && bytes.Contains(b, []byte(s))
}

// modelSince reads the files from the offsets in before (0 for files not
// listed) and returns the model of the last reply they record. strong is
// false when the only model found sits on a record that is not a reply
// (a model-change record), which says what was asked for, not what ran.
func modelSince(files map[string]int64, before map[string]int64) (model string, strong bool) {
	var lastStrong, lastAny string
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool { return modTime(paths[i]).Before(modTime(paths[j])) })
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		info, _ := f.Stat()
		off := before[p]
		if info != nil && off > info.Size() {
			off = 0
		}
		_, _ = f.Seek(off, 0)
		var docs [][]byte
		if strings.HasSuffix(p, ".json") {
			b, _ := os.ReadFile(p)
			docs = [][]byte{b}
		} else {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 16<<20)
			for sc.Scan() {
				docs = append(docs, append([]byte(nil), sc.Bytes()...))
			}
		}
		f.Close()
		for _, d := range docs {
			var v any
			if json.Unmarshal(d, &v) != nil {
				continue
			}
			walkModels(v, false, func(m string, reply bool) {
				if reply {
					lastStrong = m
				}
				lastAny = m
			})
		}
	}
	if lastStrong != "" {
		return lastStrong, true
	}
	return lastAny, false
}

var modelKeys = map[string]bool{"model": true, "modelID": true, "modelId": true, "model_id": true, "modelName": true}

// walkModels calls fn for every model id in v, in document order, saying
// whether it sits on a reply (an object with usage, or role/type
// assistant, or under such an object).
func walkModels(v any, inReply bool, fn func(string, bool)) {
	switch x := v.(type) {
	case map[string]any:
		reply := inReply
		if _, ok := x["usage"]; ok {
			reply = true
		}
		for _, k := range []string{"role", "type"} {
			if s, ok := x[k].(string); ok && (s == "assistant" || s == "turn_context" || s == "gemini" || s == "model") {
				reply = true
			}
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if modelKeys[k] {
				if s, ok := x[k].(string); ok && s != "" && !strings.HasPrefix(s, "<") {
					fn(s, reply)
				}
				continue
			}
			walkModels(x[k], reply, fn)
		}
	case []any:
		for _, e := range x {
			walkModels(e, inReply, fn)
		}
	}
}

func modTime(p string) time.Time {
	info, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
