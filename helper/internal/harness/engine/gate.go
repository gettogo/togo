package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// gateTimeout bounds one version probe.
const gateTimeout = 5 * time.Second

// gate runs an adapter's version command once per binary path and mtime.
type gate struct {
	mu    sync.Mutex
	cache map[string]gateResult
}

type gateResult struct {
	version string
	ok      bool
	reason  string
}

// check returns the harness version and whether it is inside the range.
func (g *gate) check(ctx context.Context, a *adapter, env harness.Env) gateResult {
	argv := a.Harness.Version
	if len(argv) == 0 {
		return gateResult{ok: true}
	}
	prog, err := source.LookPath(argv[0], env.Vars[source.VarPath])
	if err != nil {
		return gateResult{reason: "the harness program is not installed"}
	}
	key := prog
	if st, err := os.Stat(prog); err == nil {
		key += "@" + st.ModTime().String()
	}
	g.mu.Lock()
	if r, ok := g.cache[key]; ok {
		g.mu.Unlock()
		return r
	}
	g.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, gateTimeout)
	defer cancel()
	out, err := source.Run(ctx, append([]string{prog}, argv[1:]...), env)
	r := gateResult{}
	switch {
	case err != nil:
		r.reason = "the version command failed"
	default:
		r = judgeVersion(string(out), a.Harness.Pattern, a.Harness.Range)
	}
	if ctx.Err() == nil {
		g.mu.Lock()
		if g.cache == nil {
			g.cache = map[string]gateResult{}
		}
		g.cache[key] = r
		g.mu.Unlock()
	}
	return r
}

func judgeVersion(out, pattern, rng string) gateResult {
	v := strings.TrimSpace(out)
	if pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return gateResult{reason: "bad version pattern"}
		}
		m := re.FindStringSubmatch(v)
		if len(m) < 2 {
			return gateResult{reason: "the version was not recognised"}
		}
		v = m[1]
	}
	if rng == "" {
		return gateResult{version: v, ok: true}
	}
	cs, err := parseRange(rng)
	if err != nil {
		return gateResult{version: v, reason: "bad version range"}
	}
	ver, err := parseVersion(v)
	if err != nil {
		return gateResult{version: v, reason: "the version was not recognised"}
	}
	for _, c := range cs {
		if !c.holds(ver) {
			return gateResult{version: v, reason: "version " + v + " is outside " + rng}
		}
	}
	return gateResult{version: v, ok: true}
}

type constraint struct {
	op  string
	ver []int
}

func (c constraint) holds(v []int) bool {
	cmp := compareVersions(v, c.ver)
	switch c.op {
	case ">=":
		return cmp >= 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case "<":
		return cmp < 0
	}
	return cmp == 0
}

// parseRange reads space-separated constraints such as ">=2.1.250 <3".
func parseRange(s string) ([]constraint, error) {
	var out []constraint
	for _, f := range strings.Fields(s) {
		op := ""
		for _, p := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(f, p) {
				op = p
				break
			}
		}
		if op == "" {
			return nil, fmt.Errorf("constraint %q has no operator", f)
		}
		v, err := parseVersion(strings.TrimPrefix(f, op))
		if err != nil {
			return nil, err
		}
		out = append(out, constraint{op: op, ver: v})
	}
	if len(out) == 0 {
		return nil, errors.New("empty range")
	}
	return out, nil
}

// parseVersion reads dotted numbers; a pre-release or build suffix on the
// last part is ignored.
func parseVersion(s string) ([]int, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("version %q is not dotted numbers", s)
		}
		out = append(out, n)
	}
	return out, nil
}

func compareVersions(a, b []int) int {
	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
