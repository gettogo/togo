package daemon

import (
	"path/filepath"
	"strings"
)

// StableExecutable returns the path that keeps naming the installed togo
// across upgrades. A Homebrew binary lives at
// <prefix>/Cellar/togo/<version>/bin/togo, which brew deletes on upgrade;
// <prefix>/bin/togo is the symlink brew moves to the new version. That path
// is returned when it resolves to exe today. Any other exe is returned
// unchanged.
func StableExecutable(exe string) string {
	prefix, ok := cellarPrefix(exe)
	if !ok {
		return exe
	}
	stable := filepath.Join(prefix, "bin", "togo")
	resolved, err := filepath.EvalSymlinks(stable)
	if err != nil {
		return exe
	}
	want, err := filepath.EvalSymlinks(exe)
	if err != nil {
		want = exe
	}
	if resolved != want {
		return exe
	}
	return stable
}

// InCellar reports whether exe is a binary Homebrew installed.
func InCellar(exe string) bool {
	_, ok := cellarPrefix(exe)
	return ok
}

// cellarPrefix splits <prefix>/Cellar/togo/<version>/bin/togo and returns
// <prefix>.
func cellarPrefix(exe string) (string, bool) {
	exe = filepath.Clean(exe)
	if !filepath.IsAbs(exe) {
		return "", false
	}
	parts := strings.Split(exe, string(filepath.Separator))
	n := len(parts)
	// "", ..., "Cellar", "togo", "<version>", "bin", "togo"
	if n < 6 || parts[n-5] != "Cellar" || parts[n-4] != "togo" || parts[n-3] == "" || parts[n-2] != "bin" || parts[n-1] != "togo" {
		return "", false
	}
	return string(filepath.Separator) + filepath.Join(parts[1:n-5]...), true
}
