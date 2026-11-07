package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file that is not written yet reads as null, not as a failure: a todo
// file before the first todo, or a harness's state file before first use.
func TestMissingFileIsNull(t *testing.T) {
	env, h := testEnv(t)
	for _, path := range []string{"~/.qwen/todos/none.json", filepath.Join(h, "absent", "x.json")} {
		out, err := File{kind: "file"}.Fetch(context.Background(), sspec("s", map[string]any{"path": path}), env)
		if err != nil || string(out) != "null" {
			t.Errorf("%s = %s, %v; want null", path, out, err)
		}
	}
	// A path outside home and cwd is still refused, missing or not.
	if _, err := (File{kind: "file"}).Fetch(context.Background(), sspec("s", map[string]any{"path": "/etc/hosts"}), env); err == nil {
		t.Error("a confined read escaped")
	}
}

// A glob over a folder that does not exist lists nothing.
func TestEmptyGlobIsEmptyList(t *testing.T) {
	env, _ := testEnv(t)
	for _, g := range []string{"~/.copilot/session-state/*/events.jsonl", "~/.factory/sessions/*/*.jsonl", "~/.vibe/logs/session/*/meta.json"} {
		out, err := File{kind: "jsonl"}.Fetch(context.Background(), sspec("s", map[string]any{"glob": g}), env)
		if err != nil || string(out) != "[]" {
			t.Errorf("%s = %s, %v; want []", g, out, err)
		}
	}
}

// stderr: merge reads what a program prints on stderr (pi prints its
// version there when stdin is not a terminal); without it stderr is dropped.
func TestCLIStderrMerge(t *testing.T) {
	env, h := testEnv(t)
	bin := filepath.Join(h, "bin")
	_ = os.Mkdir(bin, 0o755)
	env.Vars[VarPath] = bin + ":/usr/bin:/bin"
	writeExe(t, bin, "errver", `echo 0.73.1 >&2`)
	ctx := context.Background()
	out, err := RunStderr(ctx, []string{"errver"}, env, true)
	if err != nil || strings.TrimSpace(string(out)) != "0.73.1" {
		t.Fatalf("merged = %q, %v", out, err)
	}
	out, err = Run(ctx, []string{"errver"}, env)
	if err != nil || len(out) != 0 {
		t.Fatalf("plain = %q, %v; want stderr dropped", out, err)
	}
	out, err = CLI{}.Fetch(ctx, sspec("s", map[string]any{"argv": []string{"errver"}, "format": "text", "stderr": "merge"}), env)
	if err != nil || !strings.Contains(string(out), "0.73.1") {
		t.Fatalf("cli merge = %s, %v", out, err)
	}
}
