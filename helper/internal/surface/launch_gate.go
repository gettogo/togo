package surface

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
)

var ompVersionLine = regexp.MustCompile(`^omp/([0-9]+)\.([0-9]+)\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?\s*$`)

// Older OMP keeps its existing chat/terminal launch. This optional integration
// uses the original TSP and query APIs checked against the 18.20 release.
func supportedOMPVersion(output string) bool {
	parts := ompVersionLine.FindStringSubmatch(strings.TrimSpace(output))
	if len(parts) != 3 {
		return false
	}
	major, err := strconv.Atoi(parts[1])
	if err != nil || major != 18 {
		return false
	}
	minor, err := strconv.Atoi(parts[2])
	return err == nil && minor >= 20
}

func nativeAllowed(ctx context.Context, launch session.Launch) bool {
	value := os.Getenv("PI_TUI_NATIVE")
	for _, entry := range state.LoginEnv(ctx) {
		if v, ok := strings.CutPrefix(entry, "PI_TUI_NATIVE="); ok {
			value = v
		}
	}
	if v, ok := launch.Env["PI_TUI_NATIVE"]; ok {
		value = v
	}
	return value != "0"
}

func ompVersion(ctx context.Context, launch session.Launch) (string, error) {
	path := state.LoginPath(ctx)
	if v, ok := launch.Env["PATH"]; ok {
		path = v
	}
	environ := append([]string(nil), state.LoginEnv(ctx)...)
	for name, value := range launch.Env {
		environ = append(environ, name+"="+value)
	}
	// Reuse the bounded argv-only runner, including process-group cancellation
	// and sibling runtimes for npm's /usr/bin/env shebang.
	out, err := source.Run(ctx, []string{launch.Program, "--version"}, harness.Env{
		Vars: map[string]string{source.VarPath: path}, Environ: environ,
	})
	return string(out), err
}
