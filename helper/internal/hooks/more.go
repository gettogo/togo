package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Harnesses whose hooks follow Claude Code's shape (droid) or Copilot
// CLI's own format. Their hooks are their native chat feed, so setup
// installs them whenever the harness is installed.
const (
	Droid   = "droid"
	Copilot = "copilot"
)

// droidEvents are Droid's hooks (Claude Code's names and shape).
var droidEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification",
	"Stop", "SubagentStop", "PreCompact", "SessionEnd"}

// copilotEvents are Copilot CLI's own hook names.
var copilotEvents = []string{"sessionStart", "userPromptSubmitted", "preToolUse", "postToolUse",
	"postToolUseFailure", "agentStop", "subagentStart", "subagentStop", "errorOccurred", "preCompact",
	"notification", "sessionEnd"}

const copilotPermission = "permissionRequest"

// droidEdits edits ~/.factory/hooks.json. Droid never waits for an answer
// from a hook, so --block adds nothing.
//
// Droid reads hooks.json as a flat map of event name to matcher groups
// ({"SessionStart": [{"hooks": [...]}]}), not Claude's {"hooks": {...}}:
// a top-level "hooks" key is rejected as an unsupported event and nothing
// in it runs. Earlier Togo builds wrote the nested shape, so every edit also
// takes Togo's entries out of a nested "hooks" object, leaving any other
// entries there for the user.
func droidEdits(o Options, add bool) ([]edit, string, error) {
	root := o.FactoryDir
	if st, err := os.Stat(root); root == "" || err != nil || !st.IsDir() {
		return nil, "", nil
	}
	s := spec{agent: Droid, binary: o.Binary, add: add, want: slices.Clone(droidEvents), all: slices.Clone(droidEvents)}
	return []edit{{path: filepath.Join(root, "hooks.json"), transform: func(cur []byte) ([]byte, error) { return editFlatHooksJSON(cur, s) }}}, root, nil
}

// legacyHooksKey is the nested object earlier Togo builds wrote Droid's
// hooks under.
const legacyHooksKey = "hooks"

// editFlatHooksJSON adds or removes togo's entries in a flat hooks document
// (event names at top level). It returns cur unchanged when there is
// nothing to do.
func editFlatHooksJSON(cur []byte, s spec) ([]byte, error) {
	top, err := parseObject(cur)
	if err != nil {
		return nil, err
	}
	unit := detectIndent(cur)
	changed := false
	if raw, ok := top.get(legacyHooksKey); ok && string(bytes.TrimSpace(raw)) != "null" {
		nested, err := parseObject(raw)
		if err != nil {
			return nil, fmt.Errorf("hooks: %w", err)
		}
		// No event wants an entry here, but on install a handler the user
		// edited by hand is still refused rather than dropped.
		drop := s
		drop.want = nil
		c, err := editEvents(&nested, drop, unit+unit, unit)
		if err != nil {
			return nil, err
		}
		if c {
			changed = true
			if len(nested) == 0 {
				top.del(legacyHooksKey)
			} else {
				top.set(legacyHooksKey, encodeObject(nested, unit, unit))
			}
		}
	}
	c, err := editEvents(&top, s, unit, unit)
	if err != nil {
		return nil, err
	}
	if !changed && !c {
		return cur, nil
	}
	return append(encodeObject(top, "", unit), '\n'), nil
}

// flatOwnedEvents lists the events of a flat hooks document that have a
// togo entry. Entries left in a legacy nested "hooks" object do not run,
// so they do not count.
func flatOwnedEvents(data []byte, agent string) (map[string]bool, error) {
	top, err := parseObject(data)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, kv := range top {
		if kv.key == legacyHooksKey {
			continue
		}
		out[kv.key] = bytes.Contains(kv.val, []byte("hook "+agent))
	}
	return out, nil
}

// legacyDroidHooks reports whether data still holds togo entries in the
// nested "hooks" object earlier builds wrote.
func legacyDroidHooks(data []byte) bool {
	top, err := parseObject(data)
	if err != nil {
		return false
	}
	raw, ok := top.get(legacyHooksKey)
	return ok && bytes.Contains(raw, []byte("hook "+Droid))
}

func claudeShapeEdits(binary, root, file, agent string, events []string, blockEvent string, block, add bool) ([]edit, string, error) {
	if st, err := os.Stat(root); root == "" || err != nil || !st.IsDir() {
		return nil, "", nil
	}
	want, all := slices.Clone(events), slices.Clone(events)
	if blockEvent != "" {
		all = append(all, blockEvent)
		if block {
			want = append(want, blockEvent)
		}
	}
	s := spec{agent: agent, binary: binary, add: add, want: want, all: all}
	return []edit{{path: filepath.Join(root, file), transform: func(cur []byte) ([]byte, error) { return editHooksJSON(cur, s) }}}, root, nil
}

// copilotEdits writes ~/.copilot/hooks/togo.json, a file of Togo's own in
// Copilot's hook format ({version: 1, hooks: {event: [{type, bash,
// timeoutSec}]}}). Uninstall deletes it.
func copilotEdits(o Options, add bool) ([]edit, string, error) {
	root := o.CopilotDir
	if st, err := os.Stat(root); root == "" || err != nil || !st.IsDir() {
		return nil, "", nil
	}
	return []edit{{path: filepath.Join(root, "hooks", "togo.json"), transform: func(cur []byte) ([]byte, error) {
		if !add {
			return nil, nil
		}
		if err := copilotEdited(cur, o.Binary); err != nil {
			return nil, err
		}
		events := slices.Clone(copilotEvents)
		if o.Block {
			events = append(events, copilotPermission)
		}
		hooks := map[string]any{}
		for _, ev := range events {
			timeout := 30
			if ev == copilotPermission {
				timeout = 600
			}
			hooks[ev] = []any{map[string]any{"type": "command", "bash": hookCommand(o.Binary, Copilot, ev), "timeoutSec": timeout}}
		}
		b, err := json.MarshalIndent(map[string]any{"version": 1, "hooks": hooks}, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	}}}, root, nil
}

// copilotEdited refuses to replace a togo.json whose hooks are no longer
// plain Togo commands: the user changed Togo's file, and install leaves it.
func copilotEdited(cur []byte, binary string) error {
	if len(cur) == 0 {
		return nil
	}
	var doc struct {
		Hooks map[string][]struct {
			Bash string `json:"bash"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(cur, &doc); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	for ev, list := range doc.Hooks {
		for _, h := range list {
			if !plainCommand(h.Bash, Copilot) {
				return fmt.Errorf("%w: hooks.%s runs %q", ErrEdited, ev, h.Bash)
			}
		}
	}
	return nil
}
