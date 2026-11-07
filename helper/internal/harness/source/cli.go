package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// ErrTooLarge is returned when a source produced more than its cap.
var ErrTooLarge = errors.New("source: output is larger than the limit")

// CLI runs a program with no shell, in the session folder, with the login
// PATH, and reads its standard output.
type CLI struct{}

func (CLI) Kind() string { return spec.KindCLI }

func (CLI) Watch(context.Context, harness.SourceSpec, harness.Env, func(harness.Record)) error {
	return harness.ErrNotStreaming
}

// Fetch runs the program until it exits or ctx ends.
func (CLI) Fetch(ctx context.Context, s harness.SourceSpec, env harness.Env) (json.RawMessage, error) {
	var c spec.CLI
	if err := json.Unmarshal(s.Raw, &c); err != nil {
		return nil, fmt.Errorf("source %s: %w", s.Name, err)
	}
	out, err := RunStderr(ctx, c.Argv, env, c.Stderr == spec.StderrMerge)
	if err != nil {
		return nil, err
	}
	return Convert(out, c.Format)
}

// Run executes argv without a shell and returns stdout, capped at
// MaxCLIOutput. Stderr is discarded.
func Run(ctx context.Context, argv []string, env harness.Env) ([]byte, error) {
	return RunStderr(ctx, argv, env, false)
}

// RunStderr is Run that, with merge, reads stderr into the same capped
// output: some harnesses print their version or lists there when stdin is
// not a terminal.
func RunStderr(ctx context.Context, argv []string, env harness.Env, merge bool) ([]byte, error) {
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("source: empty argv")
	}
	prog, err := LookPath(argv[0], env.Vars[VarPath])
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, prog, argv[1:]...)
	if env.Cwd != "" {
		cmd.Dir = Expand(env.Cwd, home(env))
	}
	cmd.Env = os.Environ()
	if p := env.Vars[VarPath]; p != "" {
		cmd.Env = append(cmd.Env, "PATH="+p)
	}
	var out cappedBuffer
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	if merge {
		cmd.Stderr = &out
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if out.over {
		return nil, ErrTooLarge
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("source: %s: %w", filepath.Base(prog), err)
	}
	return out.b.Bytes(), nil
}

// cappedBuffer keeps at most MaxCLIOutput bytes and fails the write after.
type cappedBuffer struct {
	b    bytes.Buffer
	over bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.b.Len()+len(p) > MaxCLIOutput {
		c.over = true
		return 0, ErrTooLarge
	}
	return c.b.Write(p)
}

// LookPath finds a program in path (a PATH list), or in the process PATH
// when path is empty. A name with a slash is used as given.
func LookPath(name, path string) (string, error) {
	if strings.ContainsRune(name, '/') {
		return Expand(name, ""), nil
	}
	if path == "" {
		return exec.LookPath(name)
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("source: %s: %w", name, exec.ErrNotFound)
}

// Convert turns raw output into JSON by format: json (default) is checked,
// jsonl becomes an array of its records, toml an object, text a string.
func Convert(out []byte, format string) (json.RawMessage, error) {
	switch format {
	case "", "json":
		out = bytes.TrimSpace(out)
		if !json.Valid(out) {
			return nil, errors.New("source: output is not JSON")
		}
		return out, nil
	case "jsonl":
		return json.Marshal(Records(out, MaxRecords))
	case "toml":
		var v map[string]any
		if err := toml.Unmarshal(out, &v); err != nil {
			return nil, errors.New("source: output is not TOML")
		}
		return json.Marshal(v)
	case "text":
		return json.Marshal(string(out))
	}
	return nil, fmt.Errorf("source: unknown format %q", format)
}

// Records parses JSON lines, skipping lines that do not parse, and keeps
// the last max.
func Records(b []byte, limit int) []json.RawMessage {
	var out []json.RawMessage
	for line := range bytes.SplitSeq(b, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || !json.Valid(line) {
			continue
		}
		out = append(out, json.RawMessage(line))
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	if out == nil {
		out = []json.RawMessage{}
	}
	return out
}
