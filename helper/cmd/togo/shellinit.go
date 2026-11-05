package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
)

func init() {
	register("shell-init", func(_ context.Context, args []string, env map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		return runShellInit(args, env, stdout, stderr)
	})
	addUsage("  shell-init [zsh|bash|fish]                    print a snippet that opens new terminals as sessions")
}

const shellInitHelp = `Usage: togo shell-init [zsh|bash|fish]

Prints a snippet for your shell's startup file. With the snippet in place,
an interactive shell that is not already inside tmux and has
TOGO_SHELL_INIT=1 set replaces itself with a Togo session in the current
directory, so the phone can pick the terminal up.

Nothing changes until you add the snippet yourself, for example:

  echo 'eval "$(togo shell-init zsh)"' >> ~/.zshrc

(for fish, add "togo shell-init fish | source" to config.fish)

and set TOGO_SHELL_INIT=1 where you want it, for example in the profile of
the terminal app you use at the desk. Without the variable the snippet
does nothing. The shell defaults to the one named by $SHELL.
`

// shellInitSnippets quote the directory with the shell's own "$PWD", so
// spaces, quotes and newlines in it reach togo as one argument.
var shellInitSnippets = map[string]string{
	"zsh":  posixSnippet,
	"bash": posixSnippet,
	"fish": `if status is-interactive; and test -z "$TMUX"; and test "$TOGO_SHELL_INIT" = 1; and command -sq togo
    exec togo new shell --dir "$PWD"
end
`,
}

const posixSnippet = `if [[ $- == *i* && -z ${TMUX-} && ${TOGO_SHELL_INIT-} == 1 ]] && command -v togo >/dev/null 2>&1; then
  exec togo new shell --dir "$PWD"
fi
`

func runShellInit(args []string, env map[string]string, stdout, stderr io.Writer) int {
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		fmt.Fprint(stdout, shellInitHelp)
		return exitOK
	}
	if len(args) > 1 {
		return usageError(stderr, fmt.Errorf("shell-init takes at most one shell"))
	}
	shell := filepath.Base(env["SHELL"])
	if len(args) == 1 {
		shell = args[0]
	}
	snippet, ok := shellInitSnippets[shell]
	if !ok {
		return usageError(stderr, fmt.Errorf("shell-init supports zsh, bash and fish; got %q", shell))
	}
	fmt.Fprint(stdout, snippet)
	return exitOK
}
