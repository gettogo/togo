// Command togo-release builds and checksums the helper release assets. It is a
// development tool and is not part of a release.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gettogo/togo/helper/internal/release"
)

const usage = `usage:
  togo-release build --version vX.Y.Z [--out dist] [--module-dir helper] [--go go] [--allow-dirty]
  togo-release archive --version vX.Y.Z --target os/arch --binary path [--license path] [--out dist] [--module-dir helper]
  togo-release sums <dir>
  togo-release verify --version vX.Y.Z [--pub file] [--signature-only] <dir>
  togo-release formula --version vX.Y.Z --sums dist/SHA256SUMS --base-url <releases url> [--template file]
`

var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// commands maps each subcommand to its implementation. Add new ones here.
var commands = map[string]func(args []string, stdout, stderr io.Writer) int{
	"build":   runBuild,
	"archive": runArchive,
	"sums":    runSums,
	"verify":  runVerify,
	"formula": runFormula,
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if cmd, ok := commands[args[0]]; ok {
		return cmd(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "togo-release: unknown command %q\n%s", args[0], usage)
	return 2
}

func runBuild(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.String("version", "", "release version, vX.Y.Z")
	out := fs.String("out", "dist", "output directory")
	moduleDir := fs.String("module-dir", "helper", "directory holding the helper module")
	goBin := fs.String("go", "go", "go command")
	allowDirty := fs.Bool("allow-dirty", false, "build with uncommitted changes (development only)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if !versionPattern.MatchString(*version) {
		fmt.Fprintf(stderr, "togo-release: --version must look like v1.2.3, got %q\n%s", *version, usage)
		return 2
	}
	if err := build(context.Background(), release.Config{
		Version: *version, ModuleDir: *moduleDir, OutDir: *out, GoBin: *goBin, AllowDirty: *allowDirty,
	}, stdout); err != nil {
		fmt.Fprintf(stderr, "togo-release: %v\n", err)
		return 1
	}
	return 0
}

// commitInfo fills Commit and SourceDateEpoch from the module's git history.
func commitInfo(ctx context.Context, c *release.Config) error {
	commit, err := gitOut(ctx, c.ModuleDir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	ct, err := gitOut(ctx, c.ModuleDir, "log", "-1", "--format=%ct")
	if err != nil {
		return err
	}
	c.Commit = commit
	if c.SourceDateEpoch, err = strconv.ParseInt(ct, 10, 64); err != nil {
		return fmt.Errorf("commit time %q: %w", ct, err)
	}
	return nil
}

func build(ctx context.Context, c release.Config, stdout io.Writer) error {
	dirty, err := release.IsDirty(ctx, c.ModuleDir)
	if err != nil {
		return err
	}
	if dirty && !c.AllowDirty {
		return release.CheckClean(ctx, c.ModuleDir)
	}
	if c.AllowDirty {
		// A development build never lands next to release assets.
		c.OutDir = filepath.Join(c.OutDir, "dev")
		if dirty {
			c.Version += "+dirty"
		}
	}
	if err := commitInfo(ctx, &c); err != nil {
		return err
	}
	goVersion, err := release.GoVersion(ctx, c)
	if err != nil {
		return err
	}
	license := filepath.Join(c.ModuleDir, "LICENSE")
	if _, err := os.Stat(license); err != nil {
		return err
	}
	info := release.BuildInfo{
		Version: c.Version, Commit: c.Commit, GoVersion: goVersion, Dirty: dirty,
		SourceDateEpoch: c.SourceDateEpoch, Binaries: map[string]string{},
	}
	for _, t := range release.Targets {
		bin, err := release.Build(ctx, c, t)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(bin)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		info.Binaries[t.String()] = hex.EncodeToString(sum[:])
		archive, err := release.Archive(c, t, bin, license)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, archive)
	}
	path, err := release.WriteBuildInfo(c.OutDir, info)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, path)
	return nil
}

// runArchive packs an existing binary, for example one signed after the build.
func runArchive(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.String("version", "", "release version, vX.Y.Z")
	target := fs.String("target", "", "os/arch")
	binary := fs.String("binary", "", "binary to pack as togo")
	license := fs.String("license", "", "license file (default: LICENSE in the module directory)")
	out := fs.String("out", "dist", "output directory")
	moduleDir := fs.String("module-dir", "helper", "directory holding the helper module")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *binary == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	osName, arch, ok := strings.Cut(*target, "/")
	if !versionPattern.MatchString(*version) || !ok || osName == "" || arch == "" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if *license == "" {
		*license = filepath.Join(*moduleDir, "LICENSE")
	}
	c := release.Config{Version: *version, ModuleDir: *moduleDir, OutDir: *out}
	if err := commitInfo(context.Background(), &c); err != nil {
		fmt.Fprintf(stderr, "togo-release: %v\n", err)
		return 1
	}
	path, err := release.Archive(c, release.Target{OS: osName, Arch: arch}, *binary, *license)
	if err != nil {
		fmt.Fprintf(stderr, "togo-release: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, path)
	return 0
}

func runSums(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	entries, err := os.ReadDir(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "togo-release: %v\n", err)
		return 1
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || n == release.SumsName || strings.HasSuffix(n, ".minisig") || strings.HasPrefix(n, ".") {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	path, err := release.WriteSums(args[0], names)
	if err != nil {
		fmt.Fprintf(stderr, "togo-release: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, path)
	return 0
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
