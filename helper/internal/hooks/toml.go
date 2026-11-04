package hooks

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Codex reads "hooks = true" in [features] ("codex_hooks" is the legacy
// alias). The file is parsed before and after every edit, but written
// textually so the rest of config.toml stays byte-identical, which a TOML
// round trip would not give. Any shape the textual edit cannot handle with
// certainty is refused.
const (
	tomlMarker = "# togo"
	tomlBegin  = "# togo:begin"
	tomlEnd    = "# togo:end"
	flagLine   = "hooks = true " + tomlMarker
)

var (
	plainHeader = regexp.MustCompile(`^\[features\][ \t]*(#.*)?$`)
	hooksKey    = regexp.MustCompile(`^\s*(hooks|codex_hooks)\s*=`)
)

func parseTOML(b []byte) (map[string]any, error) {
	doc := map[string]any{}
	if err := toml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse TOML: %w", err)
	}
	return doc, nil
}

// enableHooksFlag makes sure [features] enables hooks. A flag that is
// already true is left alone; any other existing hooks key is refused rather
// than overridden.
func enableHooksFlag(cur []byte) ([]byte, error) {
	before, err := parseTOML(cur)
	if err != nil {
		return nil, err
	}
	feat, has := before["features"]
	var out []byte
	switch {
	case !has:
		out = appendFeaturesBlock(cur)
	default:
		table, ok := feat.(map[string]any)
		if !ok {
			return nil, errors.New("features is not a table; enable hooks in config.toml by hand")
		}
		for _, k := range []string{"hooks", "codex_hooks"} {
			if v, ok := table[k]; ok {
				if v == true {
					return cur, nil
				}
				return nil, fmt.Errorf("features.%s is set to %v; enable hooks in config.toml by hand", k, v)
			}
		}
		out, err = insertAfterHeader(cur)
		if err != nil {
			return nil, err
		}
	}
	after, err := parseTOML(out)
	if err != nil {
		return nil, errors.New("the edit would not leave valid TOML; enable hooks in config.toml by hand")
	}
	want, _ := parseTOML(cur)
	table, _ := want["features"].(map[string]any)
	if table == nil {
		table = map[string]any{}
		want["features"] = table
	}
	table["hooks"] = true
	if !reflect.DeepEqual(after, want) {
		return nil, errors.New("config.toml has a layout togo cannot edit safely; enable hooks by hand")
	}
	return out, nil
}

func appendFeaturesBlock(cur []byte) []byte {
	s := string(cur)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if s != "" {
		s += "\n"
	}
	return []byte(s + tomlBegin + "\n[features]\nhooks = true\n" + tomlEnd + "\n")
}

// insertAfterHeader adds the flag under the one plain [features] header. Any
// other declaration of the table (quoted, dotted, inline) or a header-like
// line inside a string makes the count differ from one and is refused.
func insertAfterHeader(cur []byte) ([]byte, error) {
	lines := strings.SplitAfter(string(cur), "\n")
	header := -1
	for i, l := range lines {
		if plainHeader.MatchString(strings.TrimRight(l, "\r\n")) {
			if header >= 0 {
				return nil, errors.New("[features] appears more than once; enable hooks in config.toml by hand")
			}
			header = i
		}
	}
	if header < 0 {
		return nil, errors.New("[features] is not a plain table header; enable hooks in config.toml by hand")
	}
	eol := "\n"
	if strings.HasSuffix(lines[header], "\r\n") {
		eol = "\r\n"
	}
	if !strings.HasSuffix(lines[header], "\n") {
		lines[header] += eol
	}
	rest := append([]string{flagLine + eol}, lines[header+1:]...)
	return []byte(strings.Join(append(lines[:header+1], rest...), "")), nil
}

// disableHooksFlag removes only the line and block togo added. The result
// must parse to the same document minus features.hooks.
func disableHooksFlag(cur []byte) ([]byte, error) {
	if !strings.Contains(string(cur), tomlMarker) {
		return cur, nil
	}
	var out []string
	skipping := false
	for _, l := range strings.SplitAfter(string(cur), "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == tomlBegin:
			skipping = true
			if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) == "" {
				out = out[:n-1]
			}
		case skipping && t == tomlEnd:
			skipping = false
		case skipping:
		case strings.HasSuffix(t, tomlMarker) && hooksKey.MatchString(l):
		default:
			out = append(out, l)
		}
	}
	res := []byte(strings.Join(out, ""))
	before, err := parseTOML(cur)
	if err != nil {
		return nil, err
	}
	after, err := parseTOML(res)
	if err != nil {
		return nil, errors.New("removing togo's lines would not leave valid TOML; remove them by hand")
	}
	for _, doc := range []map[string]any{before, after} {
		if t, ok := doc["features"].(map[string]any); ok {
			delete(t, "hooks")
			if len(t) == 0 {
				delete(doc, "features")
			}
		}
	}
	if !reflect.DeepEqual(before, after) {
		return nil, errors.New("config.toml has a layout togo cannot edit safely; remove its lines by hand")
	}
	return res, nil
}
