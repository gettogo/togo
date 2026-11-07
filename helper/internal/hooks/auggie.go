package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

// Auggie is the Augment CLI. Its hooks live in ~/.augment/settings.json
// under "hooks": {event: [{matcher?, hooks: [{type, command, args}],
// metadata}]}. Auggie runs a handler with args directly, without a shell,
// so Togo's entry names the binary in command and the rest in args. The
// conversation reaches the hook only with metadata.includeConversationData.
const Auggie = "auggie"

// auggieEvents are the hooks the auggie adapter reads; Auggie has no other
// chat feed.
var auggieEvents = []string{"SessionStart", "PromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop", "SessionEnd"}

func auggieEdits(o Options, add bool) ([]edit, string, error) {
	root := o.AugmentDir
	if !dirExists(root) {
		return nil, "", nil
	}
	return []edit{{path: filepath.Join(root, "settings.json"), transform: func(cur []byte) ([]byte, error) {
		return editAuggie(cur, o.Binary, add)
	}}}, root, nil
}

// auggieGroup is Togo's matcher group for one event.
func auggieGroup(binary, event string) json.RawMessage {
	return mustJSON(map[string]any{
		"hooks":    []any{map[string]any{"type": "command", "command": binary, "args": []string{"hook", Auggie, event}}},
		"metadata": map[string]any{"includeConversationData": true},
	})
}

// auggieHandler is the part of a handler Togo reads.
type auggieHandler struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func (h auggieHandler) ours() bool {
	return len(h.Args) >= 2 && h.Args[0] == "hook" && h.Args[1] == Auggie
}

// plain reports whether the handler is exactly what auggieGroup writes for
// event, up to the binary path.
func (h auggieHandler) plain(event string) bool {
	return slices.Equal(h.Args, []string{"hook", Auggie, event}) && strings.Contains(filepath.Base(h.Command), "togo")
}

// editAuggie adds or removes Togo's group for each event and returns cur
// unchanged when there is nothing to do.
func editAuggie(cur []byte, binary string, add bool) ([]byte, error) {
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
	for _, ev := range auggieEvents {
		raw, _ := hooksObj.get(ev)
		var groups []json.RawMessage
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return nil, fmt.Errorf("hooks.%s is not an array", ev)
			}
		}
		out, c, err := editAuggieEvent(groups, binary, ev, add)
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
	if !changed {
		return cur, nil
	}
	if len(hooksObj) == 0 {
		top.del("hooks")
	} else {
		top.set("hooks", encodeObject(hooksObj, unit, unit))
	}
	return append(encodeObject(top, "", unit), '\n'), nil
}

// editAuggieEvent keeps the user's groups as they are and leaves exactly
// one Togo group when add, none otherwise.
func editAuggieEvent(groups []json.RawMessage, binary, event string, add bool) ([]json.RawMessage, bool, error) {
	want := auggieGroup(binary, event)
	var out []json.RawMessage
	changed, found := false, false
	for _, g := range groups {
		var parsed struct {
			Hooks []auggieHandler `json:"hooks"`
		}
		if err := json.Unmarshal(g, &parsed); err != nil {
			return nil, false, err
		}
		ours := slices.IndexFunc(parsed.Hooks, auggieHandler.ours) >= 0
		if !ours {
			out = append(out, g)
			continue
		}
		for _, h := range parsed.Hooks {
			if add && h.ours() && !h.plain(event) {
				return nil, false, fmt.Errorf("%w: %s %s", ErrEdited, h.Command, strings.Join(h.Args, " "))
			}
		}
		if add && !found && sameJSON(g, want) {
			found = true
			out = append(out, g)
			continue
		}
		changed = true
		if kept, ok, err := dropAuggieHandlers(g); err != nil {
			return nil, false, err
		} else if ok {
			out = append(out, kept)
		}
	}
	if add && !found {
		out = append(out, want)
		changed = true
	}
	return out, changed, nil
}

// dropAuggieHandlers takes Togo's handlers out of a group that also holds
// the user's; ok is false when none of the user's are left.
func dropAuggieHandlers(g json.RawMessage) (json.RawMessage, bool, error) {
	obj, err := parseObject(g)
	if err != nil {
		return nil, false, err
	}
	raw, _ := obj.get("hooks")
	var handlers []json.RawMessage
	if err := json.Unmarshal(raw, &handlers); err != nil {
		return nil, false, err
	}
	var kept []json.RawMessage
	for _, h := range handlers {
		var p auggieHandler
		if json.Unmarshal(h, &p) == nil && p.ours() {
			continue
		}
		kept = append(kept, h)
	}
	if len(kept) == 0 {
		return nil, false, nil
	}
	obj.set("hooks", mustJSON(kept))
	return encodeObject(obj, "", ""), true, nil
}

func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

// auggieOwnedEvents lists the events that have a Togo handler.
func auggieOwnedEvents(data []byte) (map[string]bool, error) {
	var doc struct {
		Hooks map[string][]struct {
			Hooks []auggieHandler `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for ev, groups := range doc.Hooks {
		for _, g := range groups {
			out[ev] = out[ev] || slices.IndexFunc(g.Hooks, auggieHandler.ours) >= 0
		}
	}
	return out, nil
}
