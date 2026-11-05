// Package gitx serves the git.* methods behind the phone's review tools:
// reads for any session, and stage, unstage, discard and commit for managed
// ones. Every call runs git with argv only, repository-controlled programs
// switched off, and output capped, so a hostile repository cannot make the
// helper execute anything or buffer unbounded data.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/session"
)

// MaxOutput caps diff and file content returned to a client.
const MaxOutput = 512 << 10

// listLimit caps machine-read git output such as status and tree listings.
const listLimit = 4 << 20

// Resolver finds a session by id.
type Resolver interface {
	Resolve(ctx context.Context, bibOrID string) (session.Session, error)
}

// Git runs git subcommands. The zero value uses "git" from PATH.
type Git struct {
	Bin string
	// Timeout bounds each git process; zero means DefaultTimeout.
	Timeout time.Duration
}

// DefaultTimeout is the per-process limit when Git.Timeout is unset.
const DefaultTimeout = 10 * time.Second

// ErrTimeout is wrapped by a call that ran past its time limit.
var ErrTimeout = errors.New("git timed out")

// ExitError is a git invocation that ran and exited non-zero. Its stderr
// is kept for classifying the failure and never reaches a client.
type ExitError struct {
	Code   int
	stderr []byte
}

func (e *ExitError) Error() string { return fmt.Sprintf("git exited with status %d", e.Code) }

// stderrMentions reports whether err is an *ExitError whose stderr contains
// any of the given phrases.
func stderrMentions(err error, phrases ...string) bool {
	var ee *ExitError
	if !errors.As(err, &ee) {
		return false
	}
	for _, p := range phrases {
		if bytes.Contains(ee.stderr, []byte(p)) {
			return true
		}
	}
	return false
}

// stderrLimit caps the stderr kept from one git process.
const stderrLimit = 64 << 10

// capped keeps the first stderrLimit bytes written to it and drops the rest.
type capped struct{ buf bytes.Buffer }

func (c *capped) Write(p []byte) (int, error) {
	if room := stderrLimit - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// safeArgs run before every subcommand. Each one turns off a way a
// repository's own config could make git execute a program or page output.
var safeArgs = []string{
	"--no-pager",
	"--literal-pathspecs",
	"-c", "core.pager=cat",
	"-c", "core.fsmonitor=false",
	"-c", "core.hooksPath=/dev/null",
	"-c", "core.quotepath=off",
	"-c", "diff.external=",
	"-c", "protocol.allow=never",
	"-c", "credential.helper=",
	"-c", "log.showSignature=false",
	// A submodule is another repository with its own config, which the
	// listing in neutralEnv never sees: git must not enter one. Status and
	// diff also pass --ignore-submodules=dirty so only the recorded commit
	// (the gitlink) is compared, which reads the submodule's refs and runs
	// nothing in it.
	"-c", "submodule.recurse=false",
	"-c", "diff.ignoreSubmodules=all",
	"-c", "status.submoduleSummary=false",
	"-c", "diff.submodule=short",
	// The phone's diff parser expects a/ and b/ prefixes on root-relative paths.
	"-c", "diff.noprefix=false",
	"-c", "diff.mnemonicPrefix=false",
	"-c", "diff.relative=false",
}

// ignoreSubmodules compares a submodule only by its recorded commit.
const ignoreSubmodules = "--ignore-submodules=dirty"

// Env returns the process environment with every GIT_* variable dropped and
// the variables git needs for deterministic, lock-free, prompt-free output.
func Env() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "LC_") || strings.HasPrefix(kv, "LANG=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"LC_ALL=C",
	)
}

func (g *Git) timeout() time.Duration {
	if g.Timeout > 0 {
		return g.Timeout
	}
	return DefaultTimeout
}

func (g *Git) bin() string {
	if g.Bin == "" {
		return "git"
	}
	return g.Bin
}

// Run runs `git -C dir <safe flags> args...` and returns at most limit bytes
// of stdout. When the output is longer the process is stopped and truncated
// is true. A non-zero exit is an *ExitError unless the output was truncated;
// the stdout read before it is still returned. Before every call the
// repository's hooks and filters defined in config are blanked (neutralEnv).
func (g *Git) Run(ctx context.Context, dir string, limit int, args ...string) (out []byte, truncated bool, err error) {
	return g.run(ctx, dir, nil, limit, args...)
}

// RunInput is Run with stdin fed to the process.
func (g *Git) RunInput(ctx context.Context, dir string, stdin []byte, limit int, args ...string) ([]byte, bool, error) {
	return g.run(ctx, dir, stdin, limit, args...)
}

func (g *Git) run(ctx context.Context, dir string, stdin []byte, limit int, args ...string) ([]byte, bool, error) {
	env, err := g.neutralEnv(ctx, dir)
	if err != nil {
		return nil, false, err
	}
	return g.exec(ctx, dir, stdin, env, limit, args...)
}

// exec runs one git process with optional stdin and extra environment,
// appended after Env.
func (g *Git) exec(ctx context.Context, dir string, stdin []byte, env []string, limit int, args ...string) (out []byte, truncated bool, err error) {
	name := subcommand(args)
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, g.timeout())
	defer cancel()
	argv := append([]string{"-C", dir}, safeArgs...)
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, g.bin(), argv...)
	cmd.Env = append(Env(), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr capped
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, fmt.Errorf("git %s: %w", name, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, false, fmt.Errorf("git %s: %w", name, err)
	}
	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, io.LimitReader(stdout, int64(limit)+1))
	if buf.Len() > limit {
		truncated = true
		buf.Truncate(limit)
		_ = cmd.Process.Kill()
	}
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	if copyErr != nil {
		return nil, false, fmt.Errorf("git %s: %w", name, copyErr)
	}
	if truncated {
		return buf.Bytes(), true, nil
	}
	if parent.Err() != nil {
		return nil, false, fmt.Errorf("git %s: %w", name, parent.Err())
	}
	if ctx.Err() != nil {
		return nil, false, fmt.Errorf("git %s: %w", name, ErrTimeout)
	}
	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		return buf.Bytes(), false, &ExitError{Code: ee.ExitCode(), stderr: stderr.buf.Bytes()}
	}
	if waitErr != nil {
		return nil, false, fmt.Errorf("git %s: %w", name, waitErr)
	}
	return buf.Bytes(), false, nil
}

// subcommand names the git command in args for error messages, skipping
// leading -c settings.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		return args[i]
	}
	return ""
}

// notARepoExit is git's fatal-error status, which rev-parse uses for a
// directory outside any work tree.
const notARepoExit = 128

// Toplevel returns the repository root containing dir, or "" when dir is not
// inside a work tree.
func (g *Git) Toplevel(ctx context.Context, dir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	out, _, err := g.Run(ctx, dir, listLimit, "rev-parse", "--show-toplevel")
	var ee *ExitError
	if errors.As(err, &ee) && ee.Code == notARepoExit {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// ResolveCommit returns the full hash of the commit ref names. A ref that
// begins with "-" or does not name a commit wraps fsx.ErrInvalidPath. The
// hash, not the client text, is what later argv carry.
func (g *Git) ResolveCommit(ctx context.Context, dir, ref string) (string, error) {
	if err := CheckRef(ref); err != nil {
		return "", err
	}
	out, _, err := g.Run(ctx, dir, 1024, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	var ee *ExitError
	if errors.As(err, &ee) {
		return "", fmt.Errorf("%w: ref does not resolve", fsx.ErrInvalidPath)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// CheckRef rejects refs that could be read as a git option or that carry
// control bytes, without consulting any repository.
func CheckRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\n") {
		return fmt.Errorf("%w: bad ref", fsx.ErrInvalidPath)
	}
	return nil
}

// CleanRel validates a client path as relative to the session directory and
// returns it cleaned. Absolute paths and any form that climbs out are
// rejected without touching the filesystem.
func CleanRel(p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) || filepath.IsAbs(p) || !filepath.IsLocal(p) {
		return "", fmt.Errorf("%w: %q is not a path inside the session directory", fsx.ErrInvalidPath, p)
	}
	return filepath.Clean(p), nil
}

// SessionDir returns the session's current working directory.
func SessionDir(ctx context.Context, r Resolver, id string) (string, error) {
	sess, err := r.Resolve(ctx, id)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(sess.Cwd)
	if err != nil || !info.IsDir() || !filepath.IsAbs(sess.Cwd) {
		return "", fmt.Errorf("%w: session directory unavailable", fsx.ErrInvalidPath)
	}
	return sess.Cwd, nil
}
