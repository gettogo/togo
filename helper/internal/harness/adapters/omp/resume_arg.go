package omp

import (
	"path/filepath"
	"strings"
)

// resumes reports whether the pane's start command (as tmux prints it)
// runs omp on the session file: --resume/-r or --restore with the file's
// id, a prefix of it, or its path.
func resumes(command, file string) bool {
	want := ""
	words := splitCommand(command)
	for i, w := range words {
		for _, flag := range []string{"--resume", "-r", "--restore"} {
			if w == flag && i+1 < len(words) {
				want = words[i+1]
			} else if v, ok := strings.CutPrefix(w, flag+"="); ok {
				want = v
			}
		}
	}
	if want == "" || strings.HasPrefix(want, "-") {
		return false
	}
	if strings.ContainsRune(want, '/') {
		return filepath.Clean(want) == filepath.Clean(file)
	}
	// omp names its files <time>_<id>.jsonl.
	_, id, ok := strings.Cut(strings.TrimSuffix(filepath.Base(file), ".jsonl"), "_")
	return ok && len(want) >= 4 && strings.HasPrefix(id, want)
}

// splitCommand splits a command line as tmux prints pane_start_command:
// words separated by spaces, a word with spaces or quotes in double
// quotes with backslash escapes.
func splitCommand(s string) []string {
	var out []string
	var b strings.Builder
	in, word, esc := false, false, false
	for _, r := range s {
		switch {
		case esc:
			b.WriteRune(r)
			esc = false
		case r == '\\':
			esc, word = true, true
		case r == '"':
			in, word = !in, true
		case r == ' ' && !in:
			if word {
				out = append(out, b.String())
				b.Reset()
				word = false
			}
		default:
			b.WriteRune(r)
			word = true
		}
	}
	if word {
		out = append(out, b.String())
	}
	return out
}
