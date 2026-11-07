package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// claudeBase are the events installed without --block. Notification carries
// the permission prompt when PermissionRequest is not hooked. The events
// after SessionEnd only feed the native chat and mission control: they
// change no session status.
var claudeBase = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop", "StopFailure", "SessionEnd",
	"PostToolUseFailure", "SubagentStart", "SubagentStop", "PreCompact", "PostCompact", "TaskCreated", "TaskCompleted"}

const (
	permissionRequest = "PermissionRequest"
	askUserQuestion   = "AskUserQuestion"
)

func claudeEdits(o Options, add bool) ([]edit, string, error) {
	root, path := o.ClaudeDir, filepath.Join(o.ClaudeDir, "settings.json")
	if o.Local {
		// Local mode is an explicit request for this project, so its
		// .claude directory is created when missing.
		root, path = filepath.Join(o.Cwd, ".claude"), filepath.Join(o.Cwd, ".claude", "settings.local.json")
	} else if st, err := os.Stat(root); root == "" || err != nil || !st.IsDir() {
		return nil, "", nil
	}
	// PermissionRequest is always hooked for AskUserQuestion, so the
	// phone can answer Claude's questions; --block hooks it for every tool.
	want := append(slices.Clone(claudeBase), permissionRequest)
	matchers := map[string]string{permissionRequest: askUserQuestion}
	if o.Block {
		matchers = nil
	}
	s := spec{
		agent:      Claude,
		binary:     o.Binary,
		want:       want,
		all:        append(slices.Clone(claudeBase), permissionRequest),
		statusLine: !(add && o.NoStatusLine),
		add:        add,
		matchers:   matchers,
		togoHome:   o.TogoHome,
	}
	return []edit{{path: path, transform: func(cur []byte) ([]byte, error) { return editHooksJSON(cur, s) }}}, root, nil
}

// spec says which events carry togo entries (want, when add) and which
// events togo may touch at all (all), so installing without --block removes
// a PermissionRequest entry left by an earlier --block install.
type spec struct {
	agent      string
	binary     string
	want       []string
	all        []string
	statusLine bool
	add        bool
	// matchers gives an event's togo group a matcher (Claude:
	// PermissionRequest only for AskUserQuestion without --block).
	matchers map[string]string
	// togoHome holds the chained status line's original command.
	togoHome string
}

// editHooksJSON adds or removes togo's entries in a Claude-style settings
// document (Claude settings.json and Codex hooks.json share the shape). It
// returns cur unchanged when there is nothing to do.
func editHooksJSON(cur []byte, s spec) ([]byte, error) {
	top, err := parseObject(cur)
	if err != nil {
		return nil, err
	}
	hooksRaw, hasHooks := top.get("hooks")
	hooksObj := object{}
	if hasHooks && string(bytes.TrimSpace(hooksRaw)) != "null" {
		if hooksObj, err = parseObject(hooksRaw); err != nil {
			return nil, fmt.Errorf("hooks: %w", err)
		}
	}
	unit := detectIndent(cur)
	changed := false
	for _, ev := range s.all {
		raw, ok := hooksObj.get(ev)
		var groups []json.RawMessage
		if ok {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return nil, fmt.Errorf("hooks.%s is not an array", ev)
			}
		}
		want := s.add && slices.Contains(s.want, ev)
		out, c, err := editEvent(groups, s.agent, hookCommand(s.binary, s.agent, ev), want, s.matchers[ev], s.add)
		if err != nil {
			return nil, fmt.Errorf("hooks.%s: %w", ev, err)
		}
		if !c {
			continue
		}
		changed = true
		if len(out) == 0 {
			hooksObj.del(ev)
			continue
		}
		hooksObj.set(ev, pretty(mustJSON(out), unit+unit, unit))
	}
	if len(hooksObj) == 0 {
		if hasHooks && changed {
			top.del("hooks")
		}
	} else if changed {
		top.set("hooks", encodeObject(hooksObj, unit, unit))
	}
	if s.statusLine {
		c, err := editStatusLine(&top, s, unit)
		if err != nil {
			return nil, err
		}
		changed = changed || c
	}
	if !changed {
		return cur, nil
	}
	return append(encodeObject(top, "", unit), '\n'), nil
}

// editEvent drops every togo handler in the event's groups and, when want,
// keeps exactly one with the current command. A group left with no handlers
// is dropped.
func editEvent(groups []json.RawMessage, agent, cmd string, want bool, matcher string, install bool) ([]json.RawMessage, bool, error) {
	var out []json.RawMessage
	changed, found := false, false
	for _, g := range groups {
		if !bytes.Contains(g, []byte("hook "+agent)) {
			out = append(out, g)
			continue
		}
		// A togo handler under another matcher (an earlier install with or
		// without --block) is removed and placed again under this one.
		ng, keep, c, placed, err := editGroup(g, agent, cmd, want && !found && groupMatcher(g) == matcher, install)
		if err != nil {
			return nil, false, err
		}
		changed = changed || c
		found = found || placed
		if keep {
			out = append(out, ng)
		}
	}
	if want && !found {
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd}}}
		if matcher != "" {
			group["matcher"] = matcher
		}
		out = append(out, mustJSON(group))
		changed = true
	}
	return out, changed, nil
}

// groupMatcher is a matcher group's matcher, "" when it has none.
func groupMatcher(g json.RawMessage) string {
	var m struct {
		Matcher string `json:"matcher"`
	}
	_ = json.Unmarshal(g, &m)
	return m.Matcher
}

// editGroup rewrites one matcher group. keep is false when the group ends up
// without handlers because togo's were its only ones. On install, a togo
// handler the user changed by hand is refused rather than rewritten.
func editGroup(g json.RawMessage, agent, cmd string, want, install bool) (json.RawMessage, bool, bool, bool, error) {
	obj, err := parseObject(g)
	if err != nil {
		return nil, false, false, false, err
	}
	raw, ok := obj.get("hooks")
	if !ok {
		return g, true, false, false, nil
	}
	var handlers []json.RawMessage
	if err := json.Unmarshal(raw, &handlers); err != nil {
		return nil, false, false, false, errors.New("group hooks is not an array")
	}
	var kept []json.RawMessage
	changed, placed := false, false
	for _, h := range handlers {
		hobj, err := parseObject(h)
		if err != nil {
			return nil, false, false, false, err
		}
		var c string
		if v, ok := hobj.get("command"); ok {
			_ = json.Unmarshal(v, &c)
		}
		if c == "" || !owned(c, agent) {
			kept = append(kept, h)
			continue
		}
		if install && c != cmd && !plainCommand(c, agent) {
			return nil, false, false, false, fmt.Errorf("%w: %q", ErrEdited, c)
		}
		switch {
		case want && !placed && c == cmd:
			placed = true
			kept = append(kept, h)
		case want && !placed:
			placed, changed = true, true
			hobj.set("command", mustJSON(cmd))
			kept = append(kept, encodeObject(hobj, "", ""))
		default:
			changed = true
		}
	}
	if !changed {
		return g, true, false, placed, nil
	}
	if len(kept) == 0 {
		return nil, false, true, placed, nil
	}
	obj.set("hooks", mustJSON(kept))
	return encodeObject(obj, "", ""), true, true, placed, nil
}

// ownedEvents lists the events that have a togo entry.
func ownedEvents(data []byte, agent string) (map[string]bool, error) {
	if agent == Auggie {
		return auggieOwnedEvents(data)
	}
	top, err := parseObject(data)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	raw, ok := top.get("hooks")
	if !ok {
		return out, nil
	}
	hooksObj, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	for _, kv := range hooksObj {
		out[kv.key] = bytes.Contains(kv.val, []byte("hook "+agent))
	}
	return out, nil
}

type kv struct {
	key string
	val json.RawMessage
}

// object is a JSON object that keeps key order, so an edit does not shuffle
// the user's file.
type object []kv

func (o object) get(key string) (json.RawMessage, bool) {
	for _, e := range o {
		if e.key == key {
			return e.val, true
		}
	}
	return nil, false
}

func (o *object) set(key string, val json.RawMessage) {
	for i := range *o {
		if (*o)[i].key == key {
			(*o)[i].val = val
			return
		}
	}
	*o = append(*o, kv{key, val})
}

func (o *object) del(key string) {
	*o = slices.DeleteFunc(*o, func(e kv) bool { return e.key == key })
}

// parseObject decodes a JSON object. Empty input is an empty object.
func parseObject(b []byte) (object, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return object{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("parse JSON: not an object")
	}
	o := object{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		o = append(o, kv{kt.(string), raw})
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("parse JSON: trailing data")
	}
	return o, nil
}

// encodeObject renders o with its closing brace at prefix and one unit of
// indentation per entry. Values are inserted as they are, so entries that
// were not edited keep their original bytes. An empty unit gives compact
// output.
func encodeObject(o object, prefix, unit string) json.RawMessage {
	var b bytes.Buffer
	if unit == "" {
		b.WriteByte('{')
		for i, e := range o {
			if i > 0 {
				b.WriteByte(',')
			}
			b.Write(mustJSON(e.key))
			b.WriteByte(':')
			b.Write(e.val)
		}
		b.WriteByte('}')
		return b.Bytes()
	}
	if len(o) == 0 {
		return json.RawMessage("{}")
	}
	b.WriteString("{\n")
	for i, e := range o {
		b.WriteString(prefix + unit)
		b.Write(mustJSON(e.key))
		b.WriteString(": ")
		b.Write(e.val)
		if i < len(o)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(prefix + "}")
	return b.Bytes()
}

// pretty re-indents a value that togo built or edited so it sits at prefix.
func pretty(raw json.RawMessage, prefix, unit string) json.RawMessage {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, prefix, unit); err != nil {
		return raw
	}
	return out.Bytes()
}

func mustJSON(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return bytes.TrimRight(b.Bytes(), "\n")
}

// detectIndent returns the first indentation unit found in a document, two
// spaces when it has none.
func detectIndent(doc []byte) string {
	_, rest, ok := bytes.Cut(doc, []byte("\n"))
	if !ok {
		return "  "
	}
	n := 0
	for n < len(rest) && (rest[n] == ' ' || rest[n] == '\t') {
		n++
	}
	if n == 0 || n == len(rest) {
		return "  "
	}
	return string(rest[:n])
}
