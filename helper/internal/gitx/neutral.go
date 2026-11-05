package gitx

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// neutralEnv returns GIT_CONFIG_* variables that switch off every program
// the git config at dir defines by name, for the next git process:
//
//   - Filter drivers (filter.<driver>.clean, .smudge, .process) are blanked
//     and made optional, so Git LFS and similar never run.
//   - Config-defined hooks (git 2.50+: hook.<name>.command run for each
//     hook.<name>.event) are not covered by core.hooksPath or --no-verify,
//     and fire on index and ref updates, a status refresh included. An
//     empty hook.<name>.event resets that hook's event list, so it runs
//     for nothing.
//
// The listing itself is `git config`, which runs no hook or filter. It is
// a separate process from the call it protects, so a process that rewrites
// the repository's config between the two could add a name not listed;
// such a process already runs as this user with write access to the
// repository, so this window grants it nothing new.
// Command-scope settings outrank every config file and include, and the
// environment form takes names containing "=" or dots without ambiguity.
func (g *Git) neutralEnv(ctx context.Context, dir string) ([]string, error) {
	out, _, err := g.exec(ctx, dir, nil, nil, listLimit,
		"config", "-z", "--name-only", "--get-regexp", `^(filter|hook)\.`)
	var ee *ExitError
	if errors.As(err, &ee) && ee.Code == 1 {
		return nil, nil // nothing defined
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var keys, values []string
	set := func(k, v string) { keys, values = append(keys, k), append(values, v) }
	for _, name := range strings.Split(string(out), "\x00") {
		// section.<name>.key, where <name> may be empty ("hook..command"
		// for [hook ""], which git runs like any other) or hold dots.
		section, rest, _ := strings.Cut(name, ".")
		dot := strings.LastIndexByte(rest, '.')
		if dot < 0 {
			continue
		}
		sub := section + "." + rest[:dot]
		if seen[sub] {
			continue
		}
		seen[sub] = true
		switch section {
		case "filter":
			set(sub+".clean", "")
			set(sub+".smudge", "")
			set(sub+".process", "")
			set(sub+".required", "false")
		case "hook":
			set(sub+".event", "")
			set(sub+".command", "")
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	env := []string{"GIT_CONFIG_COUNT=" + strconv.Itoa(len(keys))}
	for i := range keys {
		n := strconv.Itoa(i)
		env = append(env, "GIT_CONFIG_KEY_"+n+"="+keys[i], "GIT_CONFIG_VALUE_"+n+"="+values[i])
	}
	return env, nil
}
