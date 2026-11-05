package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/gettogo/togo/helper/internal/daemon"
	"github.com/gettogo/togo/helper/internal/release"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/update"
	"github.com/gettogo/togo/helper/internal/version"
	"github.com/gettogo/togo/helper/internal/wire"
)

func init() {
	register("update", func(ctx context.Context, args []string, _ map[string]string, _ io.Reader, stdout, stderr io.Writer) int {
		return runUpdate(ctx, args, stdout, stderr)
	})
	addUsage("  update [--check] [--version vX.Y.Z]           install the latest signed release")
}

// updateSource is where releases come from: the pinned key and the
// published URL, never the environment. Tests replace it.
var updateSource = func() (update.Source, error) {
	if err := release.CheckPinnedKey(os.Getenv); err != nil {
		return update.Source{}, err
	}
	key, err := release.ParsePublicKey(release.ReleaseKey)
	if err != nil {
		return update.Source{}, err
	}
	return update.Source{BaseURL: update.BaseURL, Key: key}, nil
}

// updateExecutable is the binary update replaces. Tests replace it.
var updateExecutable = executable

func runUpdate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("update")
	check := fs.Bool("check", false, "only report whether a newer release exists")
	want := fs.String("version", "", "install this release instead of the latest")
	allowDowngrade := fs.Bool("allow-downgrade", false, "install a --version older than this one")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) > 0 {
		return usageError(stderr, errors.New("update takes no arguments"))
	}
	if *want != "" && !update.ValidVersion(*want) {
		return usageError(stderr, fmt.Errorf("--version must look like v1.2.3, got %q", *want))
	}
	exe, err := updateExecutable()
	if err != nil {
		return fail(stderr, err)
	}
	if daemon.InCellar(exe) {
		return fail(stderr, errors.New("Installed with Homebrew: run brew upgrade togo"))
	}
	src, err := updateSource()
	if err != nil {
		return fail(stderr, err)
	}
	current := version.Current()

	target := *want
	if target == "" || *check {
		latest, newer, err := update.Check(ctx, src, current)
		if err != nil {
			return fail(stderr, err)
		}
		if *check {
			if newer {
				fmt.Fprintf(stdout, "togo %s is available (installed: %s)\n", latest, current)
			} else {
				fmt.Fprintf(stdout, "togo %s is up to date\n", current)
			}
			return exitOK
		}
		if !newer {
			fmt.Fprintf(stdout, "togo %s is up to date\n", current)
			return exitOK
		}
		target = latest
	}
	if update.Compare(target, current) < 0 && !*allowDowngrade {
		return fail(stderr, fmt.Errorf("%s is older than the installed %s; add --allow-downgrade to install it", target, current))
	}

	if err := update.Apply(ctx, src, target, exe); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Installed togo %s at %s (previous binary kept at %s)\n", target, exe, exe+update.PreviousSuffix)

	home, err := state.Home()
	if err != nil {
		return fail(stderr, err)
	}
	if err := restartDaemon(ctx, home, wire.ShutdownUpgrade, true, stdout); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}
