// Package omp is the omp (oh-my-pi) harness adapter: its adapter file, the
// omp.session mapper over session JSONL (format version 3) and the
// omp.config mapper, which reads model roles from the global config.yml.
//
// omp's rpc mode is a separate process with its own session, so the live
// TUI is read through its session files and the CLI's JSON output, and
// changed by typing its documented slash commands or with omp config set.
package omp

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	_ "github.com/gettogo/togo/helper/internal/harness/source/rpc" // the rpc runner
)

//go:embed omp.yaml
var adapterFile []byte

// Mapper names, as omp.yaml uses them.
const (
	MapperSession = "omp.session"
	MapperConfig  = "omp.config"
)

func init() {
	harness.RegisterAdapterFile("omp", adapterFile)
	harness.RegisterMapper(MapperSession, func(env harness.Env) harness.Mapper { return newSession(env) })
	harness.RegisterMapper(MapperConfig, func(env harness.Env) harness.Mapper { return newConfig(env) })
}

// AdapterFile returns the embedded adapter file.
func AdapterFile() []byte { return adapterFile }

// SessionDirNames lists the folder names omp may use under
// ~/.omp/agent/sessions for cwd, most likely first. omp names a folder
// under the temp folder "-tmp-<rel>", one under home "-<rel>", and any
// other "--<abs>--", with "/", "\" and ":" turned into "-".
func SessionDirNames(cwd, home, tmp string) []string {
	clean := func(p string) string {
		if p == "" {
			return ""
		}
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	dash := func(s string) string { return strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(s) }
	inside := func(root, p string) (string, bool) {
		if root == "" {
			return "", false
		}
		r, err := filepath.Rel(root, p)
		if err != nil || r == ".." || strings.HasPrefix(r, "../") || filepath.IsAbs(r) {
			return "", false
		}
		if r == "." {
			r = ""
		}
		return r, true
	}
	var out []string
	add := func(s string) {
		for _, o := range out {
			if o == s {
				return
			}
		}
		out = append(out, s)
	}
	for _, c := range []string{clean(cwd), filepath.Clean(cwd)} {
		if c == "" || c == "." {
			continue
		}
		for _, t := range []string{clean(tmp), "/tmp", "/private/tmp"} {
			if r, ok := inside(t, c); ok {
				if r == "" {
					add("-tmp")
				} else {
					add("-tmp-" + dash(r))
				}
			}
		}
		for _, h := range []string{clean(home), filepath.Clean(home)} {
			if r, ok := inside(h, c); ok && h != "" && h != "." {
				add("-" + dash(r))
			}
		}
		add("--" + dash(strings.TrimLeft(c, "/\\")) + "--")
	}
	return out
}

// sessionDir picks the first SessionDirNames entry that exists under the
// sessions folder, else the first one.
func sessionDir(env harness.Env) string {
	home := source.Expand("~", env.Vars[source.VarHome])
	names := SessionDirNames(env.Cwd, home, os.TempDir())
	if len(names) == 0 {
		return ""
	}
	root := filepath.Join(home, ".omp", "agent", "sessions")
	for _, n := range names {
		if st, err := os.Stat(filepath.Join(root, n)); err == nil && st.IsDir() {
			return n
		}
	}
	return names[0]
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

// stamp reads an entry timestamp: an RFC 3339 string or Unix milliseconds.
func stamp(v any, def time.Time) time.Time {
	switch x := v.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return t.UTC()
		}
	case float64:
		if x > 0 {
			return time.UnixMilli(int64(x)).UTC()
		}
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

func num(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func decode(raw json.RawMessage) map[string]any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// text joins the text parts of a content value (a string or a list of
// {type: text, text} parts).
func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	var parts []string
	for _, p := range asList(v) {
		m := obj(p)
		if str(m, "type") == "text" {
			parts = append(parts, str(m, "text"))
		}
	}
	return strings.Join(parts, "\n")
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// rel shows a path relative to the session folder when it is inside it.
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
