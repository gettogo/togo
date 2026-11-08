package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// adapter is a parsed, checked adapter file.
type adapter struct {
	*spec.Adapter
	// screenOrder and feedOrder keep the file's order, which maps lose.
	screenOrder []string
	feedOrder   []string
	origin      string
}

// loadAdapters reads the registered adapter files, then the user's
// overrides in dir (one <id>.yaml each). An override that fails to parse or
// check is logged and the built-in file stays.
func loadAdapters(dir string, logf func(string, ...any)) map[string]*adapter {
	out := map[string]*adapter{}
	files := harness.AdapterFiles()
	ids := make([]string, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		a, err := compileAdapter(files[id], "built-in")
		if err != nil {
			logf("harness: built-in adapter %s: %v", id, err)
			continue
		}
		if a.Adapter.Adapter != id {
			logf("harness: built-in adapter %s declares id %s", id, a.Adapter.Adapter)
			continue
		}
		out[id] = a
	}
	if dir == "" {
		return out
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	for _, p := range paths {
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() || st.Size() > spec.MaxFile {
			logf("harness: adapter override %s skipped", filepath.Base(p))
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		a, err := compileAdapter(b, p)
		if err != nil {
			logf("harness: adapter override %s refused: %v", filepath.Base(p), err)
			continue
		}
		if want := strings.TrimSuffix(filepath.Base(p), ".yaml"); a.Adapter.Adapter != want {
			logf("harness: adapter override %s declares id %s", filepath.Base(p), a.Adapter.Adapter)
			continue
		}
		out[a.Adapter.Adapter] = a
	}
	return out
}

// compileAdapter parses a file and checks what Parse does not: mappers and
// runners exist, jq compiles and templates parse.
func compileAdapter(b []byte, origin string) (*adapter, error) {
	a, err := spec.Parse(b)
	if err != nil {
		return nil, err
	}
	out := &adapter{Adapter: a, origin: origin}
	out.screenOrder, out.feedOrder = keyOrder(b, "screens"), keyOrder(b, "feeds")
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	for _, name := range out.feedOrder {
		f := a.Feeds[name]
		if _, ok := harness.LookupMapper(f.Mapper); !ok {
			bad("feeds.%s: unknown mapper %q", name, f.Mapper)
		}
		if _, ok := harness.LookupRunner(kindOf(f.Kinds)); !ok {
			bad("feeds.%s: no runner for %s", name, kindOf(f.Kinds))
		}
	}
	for name, s := range a.Sources {
		if _, ok := harness.LookupRunner(kindOf(s.Kinds)); !ok && kindOf(s.Kinds) != spec.KindStatic {
			bad("sources.%s: no runner for %s", name, kindOf(s.Kinds))
		}
		checkJQ(bad, "sources."+name+".select", s.Select)
	}
	for name, sc := range a.Screens {
		for k, v := range sc.Map {
			checkExpr(bad, "screens."+name+".map."+k, v)
		}
		for _, f := range sc.Fields {
			checkExpr(bad, "screens."+name+".fields."+f.ID, f.Value)
		}
	}
	for name, act := range a.Actions {
		if v := act.Verify; v != nil {
			checkJQ(bad, "actions."+name+".verify.select", v.Select)
		}
	}
	if m := a.Modes; m != nil {
		checkJQ(bad, "modes.select", m.Select)
	}
	if c := a.Commands; c != nil {
		checkJQ(bad, "commands.select", c.Select)
	}
	if _, err := parseRange(a.Harness.Range); a.Harness.Range != "" && err != nil {
		bad("harness.range: %v", err)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

func checkJQ(bad func(string, ...any), where, expr string) {
	if expr == "" {
		return
	}
	if _, err := compileJQ(expr); err != nil {
		bad("%s: %v", where, err)
	}
}

func checkExpr(bad func(string, ...any), where, expr string) {
	if expr == "" {
		return
	}
	if strings.Contains(expr, "{{") {
		if _, err := Render(expr, map[string]any{}); err != nil && strings.Contains(err.Error(), "parse") {
			bad("%s: %v", where, err)
		}
		return
	}
	checkJQ(bad, where, expr)
}

// keyOrder returns the keys of a top-level mapping in file order.
func keyOrder(b []byte, key string) []string {
	var doc yaml.Node
	if yaml.Unmarshal(b, &doc) != nil || len(doc.Content) == 0 {
		return nil
	}
	top := doc.Content[0]
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value != key {
			continue
		}
		m := top.Content[i+1]
		var out []string
		for j := 0; j+1 < len(m.Content); j += 2 {
			out = append(out, m.Content[j].Value)
		}
		return out
	}
	return nil
}

// kindOf names the one kind block set.
func kindOf(k spec.Kinds) string {
	switch {
	case k.CLI != nil:
		return spec.KindCLI
	case k.File != nil:
		return spec.KindFile
	case k.JSONL != nil:
		return spec.KindJSONL
	case k.HTTP != nil:
		return spec.KindHTTP
	case k.SSE != nil:
		return spec.KindSSE
	case k.RPC != nil:
		return spec.KindRPC
	case k.Hook != nil:
		return spec.KindHook
	case k.Statusline != nil:
		return spec.KindStatusline
	case k.SQLite != nil:
		return spec.KindSQLite
	case k.Static != nil:
		return spec.KindStatic
	}
	return ""
}

// rawOf returns the kind block as JSON for a runner.
func rawOf(k spec.Kinds) ([]byte, error) {
	var v any
	switch kindOf(k) {
	case spec.KindCLI:
		v = k.CLI
	case spec.KindFile:
		v = k.File
	case spec.KindJSONL:
		v = k.JSONL
	case spec.KindHTTP:
		v = k.HTTP
	case spec.KindSSE:
		v = k.SSE
	case spec.KindRPC:
		v = k.RPC
	case spec.KindHook:
		v = k.Hook
	case spec.KindStatusline:
		v = k.Statusline
	case spec.KindSQLite:
		v = k.SQLite
	case spec.KindStatic:
		v = k.Static
	}
	return json.Marshal(v)
}

// matches reports whether the adapter serves a session with this agent id
// and argv.
func (a *adapter) matches(agent string, argv []string) bool {
	for _, id := range a.Match.Agents {
		if id == agent {
			return true
		}
	}
	if len(argv) > 0 {
		base := filepath.Base(argv[0])
		for _, c := range a.Match.Commands {
			if c == base {
				return true
			}
		}
	}
	return false
}
