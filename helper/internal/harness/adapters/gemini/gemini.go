// Package gemini is the Gemini CLI harness adapter: its adapter file, the
// gemini.hooks mapper (session id, transcript path and the working flag
// from Gemini's hooks) and the gemini.chat mapper, which diffs the chat
// file Gemini rewrites whole after every message.
//
// Gemini has no live structured channel, so the adapter is read-mostly:
// writes are typed slash commands, settings.json writes and a relaunch
// with flags.
package gemini

import (
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

//go:embed gemini.yaml
var adapterFile []byte

// Mapper names, as gemini.yaml uses them.
const (
	MapperHooks = "gemini.hooks"
	MapperChat  = "gemini.chat"
)

func init() {
	harness.RegisterAdapterFile("gemini", adapterFile)
	harness.RegisterMapper(MapperHooks, func(env harness.Env) harness.Mapper { return newHooks(env) })
	harness.RegisterMapper(MapperChat, func(env harness.Env) harness.Mapper { return newChat(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug is the short folder name Gemini 0.30 gives a project under
// ~/.gemini/tmp when projects.json has none yet: the folder's base name,
// lower case, with runs of other characters turned into "-".
func Slug(dir string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(filepath.Base(dir)), "-"), "-")
	if s == "" {
		return "project"
	}
	return s
}

// ProjectDir reads the project's folder name from ~/.gemini/projects.json,
// falling back to Slug.
func ProjectDir(home, cwd string) string {
	cwd = filepath.Clean(cwd)
	f, err := os.Open(filepath.Join(home, ".gemini", "projects.json"))
	if err == nil {
		defer f.Close()
		var reg struct {
			Projects map[string]string `json:"projects"`
		}
		b, _ := io.ReadAll(io.LimitReader(f, 1<<20))
		if json.Unmarshal(b, &reg) == nil {
			if id := reg.Projects[cwd]; id != "" && !strings.ContainsAny(id, "/\\*?[") && id != ".." {
				return id
			}
		}
	}
	return Slug(cwd)
}

// chatGlob is where the session's chat files are when no hook has named
// the transcript yet.
func chatGlob(env harness.Env) string {
	home := source.Expand("~", env.Vars[source.VarHome])
	return "~/.gemini/tmp/" + ProjectDir(home, env.Cwd) + "/chats/session-*.json"
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

func clip(s string) string {
	if len(s) <= harness.MaxItemText {
		return s
	}
	s = s[:harness.MaxItemText]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func stamp(s string, def time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	return def
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func num(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// parts joins the text of a PartListUnion: a string, a part, or a list of
// parts and strings.
func parts(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		return str(x, "text")
	case []any:
		var out []string
		for _, p := range x {
			if t := parts(p); t != "" {
				out = append(out, t)
			}
		}
		return strings.Join(out, "\n")
	}
	return ""
}

func rel(cwd, p string) string {
	if p == "" || cwd == "" || !filepath.IsAbs(p) {
		return p
	}
	if r, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

func ptr[T any](v T) *T { return &v }
