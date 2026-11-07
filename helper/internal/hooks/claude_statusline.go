package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// statusLineState is Togo's record of a user's own status line it chained:
// the command it replaced, so uninstall puts it back exactly.
type statusLineState struct {
	Original string `json:"original"`
	Chained  string `json:"chained"`
}

func statusLineStatePath(togoHome string) string {
	return filepath.Join(togoHome, "hooks", "claude-statusline.json")
}

func loadStatusLineState(togoHome string) (statusLineState, bool) {
	var st statusLineState
	if togoHome == "" {
		return st, false
	}
	b, err := os.ReadFile(statusLineStatePath(togoHome))
	if err != nil || json.Unmarshal(b, &st) != nil || st.Chained == "" {
		return statusLineState{}, false
	}
	return st, true
}

func saveStatusLineState(togoHome string, st statusLineState) error {
	path := statusLineStatePath(togoHome)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("status line record: %w", err)
	}
	return writeFileAtomic(path, mustJSON(st), 0o600)
}

// chainCommand runs Togo's status line hook, then the user's own command
// with the same payload, printing only the user's output.
func chainCommand(binary, original string) string {
	return hookCommand(binary, Claude, "statusline") + " --then " + shellQuote(original)
}

// chained reports whether cmd is a chain Togo installed.
func chained(cmd string) bool {
	return strings.Contains(cmd, " hook "+Claude+" statusline --then ")
}

// editStatusLine installs Togo's status line. With none, Togo's own goes
// in. A user's own is chained: Togo's hook runs first, then theirs with
// the same input, and only theirs prints; the original command is kept in
// Togo's state file. Never chained twice. Uninstall puts back exactly the
// original command, and leaves a status line the user changed since.
func editStatusLine(top *object, s spec, unit string) (bool, error) {
	own := hookCommand(s.binary, s.agent, "statusline")
	raw, ok := top.get("statusLine")
	if !ok || string(bytes.TrimSpace(raw)) == "null" {
		if !s.add {
			return false, nil
		}
		top.set("statusLine", pretty(mustJSON(map[string]any{"type": "command", "command": own}), unit, unit))
		return true, nil
	}
	obj, err := parseObject(raw)
	if err != nil {
		return false, fmt.Errorf("statusLine: %w", err)
	}
	var c string
	if v, ok := obj.get("command"); ok {
		_ = json.Unmarshal(v, &c)
	}
	set := func(cmd string) {
		obj.set("command", mustJSON(cmd))
		top.set("statusLine", pretty(encodeObject(obj, "", ""), unit, unit))
	}
	switch {
	case chained(c):
		st, known := loadStatusLineState(s.togoHome)
		if !s.add {
			if !known || st.Chained != c {
				return false, nil // edited since, or not recorded: leave it
			}
			set(st.Original)
			_ = os.Remove(statusLineStatePath(s.togoHome))
			return true, nil
		}
		if !known {
			return false, nil
		}
		want := chainCommand(s.binary, st.Original)
		if c == want {
			return false, nil
		}
		if st.Chained != c {
			return false, nil // the user edited the chain: leave it
		}
		if err := saveStatusLineState(s.togoHome, statusLineState{Original: st.Original, Chained: want}); err != nil {
			return false, err
		}
		set(want)
		return true, nil
	case owned(c, s.agent):
		if !s.add {
			top.del("statusLine")
			return true, nil
		}
		if c == own {
			return false, nil
		}
		set(own)
		return true, nil
	}
	// The user's own status line.
	if !s.add || c == "" || s.togoHome == "" {
		return false, nil
	}
	want := chainCommand(s.binary, c)
	if err := saveStatusLineState(s.togoHome, statusLineState{Original: c, Chained: want}); err != nil {
		return false, err
	}
	set(want)
	return true, nil
}
