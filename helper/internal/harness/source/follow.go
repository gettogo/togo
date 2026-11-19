package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// VarTogoHome is the helper's own folder (~/.togo); a session's followed
// file is remembered under its run folder.
const VarTogoHome = "togo_home"

// VarTTY is the terminal device of the session's pane (/dev/ttys011), when
// the helper knows it.
const VarTTY = "tty"

// followFile is where a session's followed file is remembered, "" when the
// environment has no folder or the id is not a plain name.
func followFile(env harness.Env) string {
	home, id := env.Vars[VarTogoHome], env.SessionID
	if home == "" || id == "" || id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return ""
	}
	return filepath.Join(home, "run", "follow-"+id+".json")
}

// saveFollow remembers the file a session follows so that, after a helper
// restart, it goes back to it instead of guessing. Best effort.
func saveFollow(env harness.Env, path string) {
	p := followFile(env)
	if p == "" {
		return
	}
	b, _ := json.Marshal(struct {
		Path string `json:"path"`
	}{path})
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// loadFollow returns the remembered file, "" when there is none.
func loadFollow(env harness.Env) string {
	p := followFile(env)
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var v struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	return v.Path
}

// SaveFollowed remembers, under the name of the thing followed, which
// conversation a session's mapper follows (a database row, not a file), so
// a helper restart goes back to it: run/follow-<session>.<name>.json, 0600.
// An empty value forgets it. Best effort.
func SaveFollowed(env harness.Env, name, value string) {
	p := followFile(env)
	if p == "" || name == "" || name != filepath.Base(name) {
		return
	}
	p = strings.TrimSuffix(p, ".json") + "." + name + ".json"
	if value == "" {
		_ = os.Remove(p)
		return
	}
	b, _ := json.Marshal(struct {
		ID string `json:"id"`
	}{value})
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// LoadFollowed returns what SaveFollowed remembered, "" when nothing.
func LoadFollowed(env harness.Env, name string) string {
	p := followFile(env)
	if p == "" || name == "" || name != filepath.Base(name) {
		return ""
	}
	b, err := os.ReadFile(strings.TrimSuffix(p, ".json") + "." + name + ".json")
	if err != nil {
		return ""
	}
	var v struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	return v.ID
}

// RemoveFollows forgets everything a removed session followed.
func RemoveFollows(togoHome, id string) {
	p := followFile(harness.Env{SessionID: id, Vars: map[string]string{VarTogoHome: togoHome}})
	if p == "" {
		return
	}
	_ = os.Remove(p)
	more, _ := filepath.Glob(strings.TrimSuffix(p, ".json") + ".*.json")
	for _, m := range more {
		_ = os.Remove(m)
	}
}

// nameStamp matches the creation time in the file names of OMP and Pi:
// 2026-11-12T15-57-52-145Z_<id>.jsonl.
var nameStamp = regexp.MustCompile(`^(\d{4}-\d\d-\d\d)T(\d\d)-(\d\d)-(\d\d)-(\d{3})Z_`)

// birth is when a file was created: the stamp in its name when it has one,
// else the file system's birth time where it keeps one. Zero when unknown.
func birth(path string, st os.FileInfo) time.Time {
	if m := nameStamp.FindStringSubmatch(filepath.Base(path)); m != nil {
		if t, err := time.Parse("2006-01-02T15:04:05.000Z", m[1]+"T"+m[2]+":"+m[3]+":"+m[4]+"."+m[5]+"Z"); err == nil {
			return t
		}
	}
	return birthTime(st)
}
