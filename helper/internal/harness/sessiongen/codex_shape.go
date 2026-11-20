package sessiongen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CodexShapes is what the real app-server recordings hold, per message
// kind: every field path with the JSON kinds seen there, and the values
// seen in fields that name a state or a type. The Codex profile's journal
// and the test app-server's answers are checked against it: a message may
// carry fewer fields than a recording, never one no recording has, of
// another kind, or with a state no recording shows.
type CodexShapes struct {
	paths  map[string]map[string]bool // path -> kinds
	values map[string]map[string]bool // enum path -> values
	kinds  map[string]bool            // message kinds
}

// enumFields name a state or a type: their values must be recorded ones.
var enumFields = map[string]bool{"status": true, "type": true, "phase": true, "kind": true, "tool": true,
	"codexErrorInfo": true, "source": true, "itemsView": true}

// LoadCodexShapes reads every recording in dir (adapters/codex/testdata).
func LoadCodexShapes(dir string) (*CodexShapes, error) {
	s := &CodexShapes{paths: map[string]map[string]bool{}, values: map[string]map[string]bool{}, kinds: map[string]bool{}}
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no Codex recordings in %s", dir)
	}
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			if inner, ok := m["msg"].(map[string]any); ok { // permissions-0167.jsonl
				if m["dir"] != "in" {
					continue
				}
				m = inner
			}
			kind, root := codexKind(m)
			if kind == "" {
				continue
			}
			s.kinds[kind] = true
			codexWalk(kind, root, func(path, k string, v any) {
				if s.paths[path] == nil {
					s.paths[path] = map[string]bool{}
				}
				s.paths[path][k] = true
				if str, ok := v.(string); ok && enumFields[codexLast(path)] {
					if s.values[path] == nil {
						s.values[path] = map[string]bool{}
					}
					s.values[path][str] = true
				}
			})
		}
		fh.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// codexKind names a message: its method (a request's prefixed "request "),
// or "resume result" for the answer to thread/resume, which recordings
// hold as a response or as the feed's {"method":"thread/resume",
// "params":{"result"}} record. The root is what is compared.
func codexKind(m map[string]any) (string, any) {
	method, _ := m["method"].(string)
	_, hasID := m["id"]
	switch {
	case method == "thread/resume":
		if p, ok := m["params"].(map[string]any); ok {
			return "resume result", p["result"]
		}
	case method != "" && hasID:
		return "request " + method, m["params"]
	case method != "":
		return method, m["params"]
	case m["result"] != nil:
		if r, ok := m["result"].(map[string]any); ok && r["thread"] != nil {
			return "resume result", r
		}
	}
	return "", nil
}

// Check lists how one message differs from the recordings.
func (s *CodexShapes) Check(msg map[string]any) []string {
	raw, err := json.Marshal(msg)
	if err != nil {
		return []string{err.Error()}
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	kind, root := codexKind(m)
	if kind == "" {
		return []string{"not a message the recordings hold"}
	}
	if !s.kinds[kind] {
		return []string{kind + ": no recording holds this message"}
	}
	var out []string
	codexWalk(kind, root, func(path, k string, v any) {
		switch {
		case s.paths[path] == nil:
			out = append(out, path+": no recording has this field")
		case !s.paths[path][k]:
			out = append(out, fmt.Sprintf("%s: %s, recorded as %v", path, k, codexKeys(s.paths[path])))
		}
		if str, ok := v.(string); ok && enumFields[codexLast(path)] && s.values[path] != nil && !s.values[path][str] {
			out = append(out, fmt.Sprintf("%s: %q, recorded values %v", path, str, codexKeys(s.values[path])))
		}
	})
	return out
}

// CheckLine is Check over one JSON line.
func (s *CodexShapes) CheckLine(line []byte) []string {
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		return []string{err.Error()}
	}
	return s.Check(m)
}

// walk visits every leaf and container with a normalised path: a thread,
// a turn and an item are compared wherever they appear (an item by its
// type), and the keys of agentsStates (thread ids) are one key.
func codexWalk(kind string, v any, visit func(path, kind string, v any)) {
	var rec func(path string, v any)
	rec = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			visit(path, "object", v)
			for k, c := range x {
				p := path + "." + k
				if codexLast(path) == "agentsStates" {
					p = path + ".*"
				}
				switch {
				case k == "thread":
					p = "THREAD"
				case k == "turn":
					p = "TURN"
				case k == "item":
					if it, ok := c.(map[string]any); ok && it["type"] != nil {
						p = "ITEM " + fmt.Sprint(it["type"])
					}
				}
				rec(p, c)
			}
		case []any:
			visit(path, "array", v)
			for _, c := range x {
				p := path + "[]"
				switch {
				case codexLast(path) == "turns":
					p = "TURN"
				case codexLast(path) == "items":
					if it, ok := c.(map[string]any); ok {
						p = "ITEM " + fmt.Sprint(it["type"])
					}
				}
				rec(p, c)
			}
		case string:
			visit(path, "string", v)
		case float64, int, int64:
			visit(path, "number", v)
		case bool:
			visit(path, "bool", v)
		case nil:
			visit(path, "null", v)
		default:
			visit(path, fmt.Sprintf("%T", v), v)
		}
	}
	rec(kind, v)
}

func codexLast(path string) string {
	if i := strings.LastIndexAny(path, ". "); i >= 0 {
		return strings.TrimSuffix(path[i+1:], "[]")
	}
	return path
}

func codexKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
