package surface

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// MaxCommandsBytes bounds the command list a companion publishes.
const MaxCommandsBytes = 256 << 10

// CompanionCommand is one slash command as the running program's own editor
// lists it after "/": every command of this session, with the ones its RPC
// list leaves out, and the current value where the program shows one.
type CompanionCommand struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	State  string `json:"state,omitempty"`
	Icon   string `json:"icon,omitempty"`
	// Group is the folder entry the command sits under ("skill:").
	Group string `json:"group,omitempty"`
	// Folder marks an entry that only groups other commands.
	Folder bool `json:"folder,omitempty"`
}

// Commands is the command list of the session's running program, or false
// when no live companion of this run has published one: an older program, a
// launch without extensions, or a process that has ended.
func (c *Companions) Commands(id string) ([]CompanionCommand, bool) {
	config, err := c.Current(id)
	if err != nil {
		return nil, false
	}
	if state, err := c.State(config); err != nil || !state.Available {
		return nil, false
	}
	f, err := os.Open(filepath.Join(filepath.Dir(config.Path), "commands.json"))
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || st.Size() > MaxCommandsBytes {
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxCommandsBytes+1))
	if err != nil || len(raw) > MaxCommandsBytes {
		return nil, false
	}
	var list struct {
		Version    int                `json:"version"`
		Generation string             `json:"generation"`
		Commands   []CompanionCommand `json:"commands"`
	}
	if json.Unmarshal(raw, &list) != nil || list.Version != 1 || list.Generation != config.Generation || len(list.Commands) == 0 {
		return nil, false
	}
	return list.Commands, true
}
