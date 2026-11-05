package release

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Target is an operating system and architecture the helper is released for.
type Target struct{ OS, Arch string }

// String returns the target as "os/arch".
func (t Target) String() string { return t.OS + "/" + t.Arch }

// Targets lists every platform a release covers.
var Targets = []Target{{"darwin", "arm64"}, {"darwin", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}}

const versionSymbol = "github.com/gettogo/togo/helper/internal/version.Version"

// Config describes one release build.
type Config struct {
	Version         string // vX.Y.Z, linked into the binary
	Commit          string // full commit hash, recorded in BUILDINFO.json
	ModuleDir       string // directory holding the helper's go.mod
	OutDir          string // where binaries, archives and metadata are written
	GoBin           string // go command; "go" when empty
	SourceDateEpoch int64  // commit time in seconds
	AllowDirty      bool   // build with uncommitted changes (development only)
}

func (c Config) goBin() string {
	if c.GoBin == "" {
		return "go"
	}
	return c.GoBin
}

// BinPath is where Build places the binary for t.
func (c Config) BinPath(t Target) string {
	out, err := filepath.Abs(c.OutDir)
	if err != nil {
		out = c.OutDir
	}
	return filepath.Join(out, "bin", t.OS+"_"+t.Arch, "togo")
}

// Build compiles cmd/togo for t and returns the path of the binary.
func Build(ctx context.Context, c Config, t Target) (string, error) {
	if !c.AllowDirty {
		if err := CheckClean(ctx, c.ModuleDir); err != nil {
			return "", err
		}
	}
	pin, err := Toolchain(c.ModuleDir)
	if err != nil {
		return "", err
	}
	bin := c.BinPath(t)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", err
	}
	args := []string{
		"build", "-trimpath", "-buildvcs=false", "-mod=readonly",
		"-ldflags", "-s -w -buildid= -X " + versionSymbol + "=" + c.Version,
		"-o", bin, "./cmd/togo",
	}
	cmd := exec.CommandContext(ctx, c.goBin(), args...)
	cmd.Dir = c.ModuleDir
	cmd.Env = buildEnv(os.Environ(), t, c.SourceDateEpoch, pin)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build %s: %w\n%s", t, err, strings.TrimSpace(string(out)))
	}
	return bin, nil
}

// buildEnv returns base with every variable that could change the output
// replaced by a fixed value.
func buildEnv(base []string, t Target, epoch int64, pin string) []string {
	fixed := map[string]string{
		"GOOS":              t.OS,
		"GOARCH":            t.Arch,
		"CGO_ENABLED":       "0",
		"GOFLAGS":           "",
		"GOWORK":            "off",
		"GOTOOLCHAIN":       pin,
		"SOURCE_DATE_EPOCH": strconv.FormatInt(epoch, 10),
	}
	dropped := map[string]bool{"GOAMD64": true, "GOARM64": true, "GOEXPERIMENT": true, "GOROOT": true}
	env := make([]string, 0, len(base)+len(fixed))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := fixed[k]; ok || dropped[k] {
			continue
		}
		env = append(env, kv)
	}
	for _, k := range []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOWORK", "GOTOOLCHAIN", "SOURCE_DATE_EPOCH"} {
		env = append(env, k+"="+fixed[k])
	}
	return env
}

// CheckClean fails when the working tree in dir has uncommitted changes.
func CheckClean(ctx context.Context, dir string) error {
	dirty, err := IsDirty(ctx, dir)
	if err != nil {
		return err
	}
	if dirty {
		return errors.New("working tree has uncommitted changes; commit them or pass --allow-dirty for a development build")
	}
	return nil
}

// IsDirty reports whether the working tree in dir has uncommitted changes.
func IsDirty(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("check working tree in %s: %w", dir, err)
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

var toolchainLine = regexp.MustCompile(`(?m)^toolchain[ \t]+(go[0-9][^ \t\r\n]*)`)

// Toolchain returns the Go toolchain pinned by the toolchain line of the
// go.mod in moduleDir.
func Toolchain(moduleDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		return "", err
	}
	m := toolchainLine.FindSubmatch(data)
	if m == nil {
		return "", errors.New("go.mod pins no toolchain; add a toolchain line")
	}
	return string(m[1]), nil
}

// GoVersion runs the go command the way builds do, in c.ModuleDir with the
// pinned toolchain selected, and fails unless it is exactly the pin.
func GoVersion(ctx context.Context, c Config) (string, error) {
	pin, err := Toolchain(c.ModuleDir)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, c.goBin(), "env", "GOVERSION")
	cmd.Dir = c.ModuleDir
	cmd.Env = buildEnv(os.Environ(), Target{runtime.GOOS, runtime.GOARCH}, 0, pin)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go env GOVERSION with %s: %w", pin, err)
	}
	got := strings.TrimSpace(string(out))
	if got != pin {
		return "", fmt.Errorf("go reports %s but go.mod pins %s", got, pin)
	}
	return got, nil
}
