// Package source holds the runners that read an adapter's sources and
// feeds: cli (a program run without a shell), file and jsonl (read, or
// tailed with polling), hook and statusline (payloads the togo hook command
// forwards) and static (data written in the adapter file). The engine
// renders every template in a source's block before a runner sees it, so
// runners only execute and confine.
package source

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Limits.
const (
	MaxCLIOutput = 1 << 20 // stdout of a cli source
	MaxFileRead  = 8 << 20 // one read of a file or jsonl source
	MaxRecords   = 2000    // records replayed from a jsonl tail
	MaxGlobFiles = 200     // files a glob lists
)

// Vars an engine puts in Env.Vars for runners.
const (
	VarHome = "home" // the user's home, for ~ and confinement
	VarPath = "path" // the login PATH programs are looked up in
)

// ErrConfined is returned for a path outside the home and the session cwd.
var ErrConfined = errors.New("source: path is outside the home folder and the session folder")

func init() {
	harness.RegisterRunner(CLI{})
	harness.RegisterRunner(File{kind: spec.KindFile})
	harness.RegisterRunner(File{kind: spec.KindJSONL})
	harness.RegisterRunner(Hook{kind: spec.KindHook})
	harness.RegisterRunner(Hook{kind: spec.KindStatusline})
	harness.RegisterRunner(Static{})
	harness.RegisterRunner(SQLite{})
}

func home(env harness.Env) string {
	if h := env.Vars[VarHome]; h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// Expand turns a leading ~ into the home folder.
func Expand(p, home string) string {
	if p == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return p
}

// Confine resolves p (after ~ expansion) through symlinks and checks it
// lies under the home folder or the session cwd. It returns the resolved
// path.
func Confine(p string, env harness.Env) (string, error) {
	h := home(env)
	p = Expand(p, h)
	if !filepath.IsAbs(p) {
		if env.Cwd == "" {
			return "", ErrConfined
		}
		p = filepath.Join(Expand(env.Cwd, h), p)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	for _, root := range []string{h, Expand(env.Cwd, h)} {
		if root == "" {
			continue
		}
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if real == r || strings.HasPrefix(real, r+string(filepath.Separator)) {
			return real, nil
		}
	}
	return "", ErrConfined
}

// Static serves data written in the adapter file.
type Static struct{}

func (Static) Kind() string { return spec.KindStatic }

func (Static) Fetch(_ context.Context, s harness.SourceSpec, _ harness.Env) (json.RawMessage, error) {
	if len(s.Raw) == 0 {
		return json.RawMessage("null"), nil
	}
	return s.Raw, nil
}

func (Static) Watch(context.Context, harness.SourceSpec, harness.Env, func(harness.Record)) error {
	return harness.ErrNotStreaming
}
