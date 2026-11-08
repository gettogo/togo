package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// Cursor is Cursor CLI (cursor-agent). Its user hooks live in
// ~/.cursor/hooks.json as {version: 1, hooks: {event: [{command, ...}]}};
// it runs each command through a shell with the payload on stdin (read
// from the 2026.11.05 bundle). The file is shared with the user's own
// hooks, so Togo adds and removes only its own entries.
const Cursor = "cursor"

// cursorEvents are the hooks the cursor adapter reads, in Cursor's names.
var cursorEvents = []string{"sessionStart", "beforeSubmitPrompt", "preToolUse", "postToolUse",
	"postToolUseFailure", "afterAgentResponse", "stop", "subagentStart", "subagentStop", "preCompact",
	"sessionEnd"}

func cursorEdits(o Options, add bool) ([]edit, string, error) {
	root := o.CursorDir
	if !dirExists(root) {
		return nil, "", nil
	}
	return []edit{{path: filepath.Join(root, "hooks.json"), transform: func(cur []byte) ([]byte, error) {
		return editCursor(cur, o.Binary, add)
	}}}, root, nil
}

// editCursor leaves exactly one Togo entry per event when add, none
// otherwise, keeps every other entry and key as it is, and returns cur
// unchanged when there is nothing to do.
func editCursor(cur []byte, binary string, add bool) ([]byte, error) {
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
	for _, ev := range cursorEvents {
		raw, _ := hooksObj.get(ev)
		var entries []json.RawMessage
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &entries); err != nil {
				return nil, fmt.Errorf("hooks.%s is not an array", ev)
			}
		}
		want := mustJSON(map[string]any{"command": hookCommand(binary, Cursor, ev)})
		var out []json.RawMessage
		found, c := false, false
		for _, e := range entries {
			var h struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(e, &h) != nil || !owned(h.Command, Cursor) {
				out = append(out, e)
				continue
			}
			if add && !plainCommand(h.Command, Cursor) {
				return nil, fmt.Errorf("%w: hooks.%s runs %q", ErrEdited, ev, h.Command)
			}
			if add && !found && sameJSON(e, want) {
				found = true
				out = append(out, e)
				continue
			}
			c = true
		}
		if add && !found {
			out = append(out, want)
			c = true
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
	if _, ok := top.get("version"); !ok && add {
		top = append(object{{key: "version", val: json.RawMessage("1")}}, top...)
	}
	if len(hooksObj) == 0 {
		top.del("hooks")
	} else {
		top.set("hooks", encodeObject(hooksObj, unit, unit))
	}
	return append(encodeObject(top, "", unit), '\n'), nil
}
