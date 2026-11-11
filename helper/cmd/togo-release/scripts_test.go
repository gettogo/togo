package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const scriptDir = "../../../scripts/release"

// fakeTool writes an executable that appends its argv, one argument per
// line, to log.
func fakeTool(t *testing.T, dir, name, log string) {
	t.Helper()
	body := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> '" + log + "'; done\nprintf -- '--\\n' >> '" + log + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runScript(t *testing.T, env []string, name string, args ...string) (string, error) {
	return runScriptIn(t, scriptDir, env, name, args...)
}

func runScriptIn(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(dir, name)}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestSignDarwinPassesRequirementText(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "codesign.log")
	fakeTool(t, bin, "codesign", log)
	// The script refuses to sign off macOS; pretend to be Darwin so the
	// requirement text is checked on every platform.
	if err := os.WriteFile(filepath.Join(bin, "uname"), []byte("#!/bin/sh\necho Darwin\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "togo")
	if err := os.WriteFile(target, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runScript(t, []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "TOGO_SIGN_NAME=Jane Doe"}, "sign-darwin.sh", target, "--run")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Split(strings.TrimSuffix(string(data), "--\n"), "--\n")
	if len(calls) != 3 {
		t.Fatalf("codesign ran %d times: %q", len(calls), data)
	}
	want := []string{
		"--force\n--options\nruntime\n--timestamp\n--sign\nDeveloper ID Application: Jane Doe (WULW83YF8M)\n-i\napp.togo.helper\n" + target + "\n",
		"--verify\n--strict\n--verbose=2\n" + target + "\n",
		"--verify\n--strict\n-R\n=anchor apple generic and certificate leaf[subject.OU] = \"WULW83YF8M\"\n" + target + "\n",
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call %d:\n got %q\nwant %q", i, calls[i], want[i])
		}
	}
}

// testKeyRepo returns a scripts directory inside a throwaway repo layout whose
// pinned release key is the test key, whatever the real repo pins.
func testKeyRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	scripts := filepath.Join(root, "scripts", "release")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sign-sums.sh", "build-release.sh"} {
		b, err := os.ReadFile(filepath.Join(scriptDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scripts, name), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pubDir := filepath.Join(root, "helper", "internal", "release")
	if err := os.MkdirAll(pubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pub, err := os.ReadFile("../../internal/release/testdata/test.pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pubDir, "minisign.pub"), pub, 0o644); err != nil {
		t.Fatal(err)
	}
	return scripts
}

func TestReleaseScriptsRefusePinnedTestKey(t *testing.T) {
	scripts := testKeyRepo(t)
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "minisign.key")
	if err := os.WriteFile(key, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"TOGO_RELEASE_TEST=", "TOGO_MINISIGN_KEY=" + key}
	for _, c := range [][]string{
		{"sign-sums.sh", dist, "v1.0.0", "--run"},
		{"build-release.sh", "v1.0.0", "--run", "--skip-apple"},
	} {
		out, err := runScriptIn(t, scripts, env, c[0], c[1:]...)
		if err == nil || !strings.Contains(out, "test key") {
			t.Errorf("%v: err %v, output %q", c, err, out)
		}
	}
}

func TestCheckActionsPinned(t *testing.T) {
	script := "../../../scripts/check-actions-pinned.sh"
	sha := strings.Repeat("a", 40)
	cases := []struct {
		name, body, flag string
		ok               bool
	}{
		{"pinned", "steps:\n  - uses: actions/checkout@" + sha + " # v4.1.0\n", "", true},
		{"tag", "steps:\n  - uses: actions/checkout@v4\n", "", false},
		{"short sha", "steps:\n  - uses: actions/checkout@abc1234 # v4\n", "", false},
		{"no comment", "steps:\n  - uses: actions/checkout@" + sha + "\n", "", false},
		{"local", "steps:\n  - uses: ./.github/actions/x\n", "", true},
		{"placeholder", "steps:\n  - uses: a/b@" + strings.Repeat("0", 40) + " # v1.0.0\n", "", true},
		{"placeholder resolved", "steps:\n  - uses: a/b@" + strings.Repeat("0", 40) + " # v1.0.0\n", "--resolved", false},
	}
	for _, c := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "w.yml"), []byte(c.body), 0o644); err != nil {
			t.Fatal(err)
		}
		args := []string{script}
		if c.flag != "" {
			args = append(args, c.flag)
		}
		out, err := exec.Command("bash", append(args, dir)...).CombinedOutput()
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v out=%s", c.name, err, out)
		}
	}
}
