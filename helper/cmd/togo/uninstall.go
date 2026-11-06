package main

import (
	"context"
	"fmt"
	"io"

	"github.com/gettogo/togo/helper/internal/hooks"
	"github.com/gettogo/togo/helper/internal/state"
)

func init() {
	register("uninstall", func(ctx context.Context, args []string, env map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		return runUninstall(ctx, args, env, stdout, stderr)
	})
	addUsage("  uninstall                                     remove agent hooks and the login service")
}

// runUninstall undoes what setup added outside $TOGO_HOME. It keeps going
// after a failure so one broken config does not leave the service behind.
func runUninstall(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return usageError(stderr, fmt.Errorf("uninstall takes no arguments"))
	}
	code := exitOK
	if o, err := hookOptions(env, false, false); err != nil {
		code = fail(stderr, err)
	} else if c := printHookResults(hooks.Uninstall(o, hooks.Agents), stdout, stderr); c != exitOK {
		code = c
	}
	svc, err := userService()
	if err != nil {
		return fail(stderr, err)
	}
	if err := uninstallService(ctx, svc, stderr); err != nil {
		code = fail(stderr, err)
	} else {
		fmt.Fprintln(stdout, "service: uninstalled")
	}
	if home, err := state.Home(); err == nil {
		if c := uninstallAwake(ctx, home, stdout, stderr); c != exitOK {
			code = c
		}
		fmt.Fprintf(stdout, "Left %s in place; delete it to remove sessions and keys.\n", home)
	}
	return code
}
