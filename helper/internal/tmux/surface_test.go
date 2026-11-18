package tmux_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/tmux"
	"github.com/gettogo/togo/helper/internal/tmux/tmuxtest"
)

func surfaceOption(t *testing.T, c *tmux.Client, key string) string {
	t.Helper()
	args := append(append([]string{}, c.Socket...), "show-options", "-qv", "-t", "=surface:", key)
	raw, err := exec.Command(c.Bin, args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

func TestSurfaceMarkerRestoresPriorValueAndProtectsNewOwner(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: "surface", Command: []string{"/bin/sh"}}); err != nil {
		t.Fatal(err)
	}
	for _, prior := range []string{"", "external-value"} {
		if prior != "" {
			if err := c.SetOption(ctx, "surface", "@tern-tsp", prior); err != nil {
				t.Fatal(err)
			}
		}
		first, second := strings.Repeat("a", 64), strings.Repeat("b", 64)
		if err := c.ClaimSurface(ctx, "surface", first); err != nil {
			t.Fatal(err)
		}
		if got := surfaceOption(t, c, "@tern-tsp"); got != "1" {
			t.Fatal("native discovery missing", got)
		}
		if err := c.ClaimSurface(ctx, "surface", second); err != nil {
			t.Fatal(err)
		}
		if err := c.ReleaseSurface(ctx, "surface", first); err != nil {
			t.Fatal(err)
		}
		if got := surfaceOption(t, c, "@tern-tsp"); got != "1" {
			t.Fatal("old owner removed the new renderer", got)
		}
		if err := c.ReleaseSurface(ctx, "surface", second); err != nil {
			t.Fatal(err)
		}
		if got := surfaceOption(t, c, "@tern-tsp"); got != prior {
			t.Fatalf("prior marker not restored: %q != %q", got, prior)
		}
	}
}

func TestSurfaceStartupClearsOnlyHelperOwnedMarkers(t *testing.T) {
	c := tmuxtest.New(t)
	ctx := context.Background()
	if err := c.NewSession(ctx, tmux.NewSessionOpts{Name: "surface", Command: []string{"/bin/sh"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOption(ctx, "surface", "@tern-tsp", "external-value"); err != nil {
		t.Fatal(err)
	}
	if err := c.ReleaseStaleSurfaces(ctx); err != nil {
		t.Fatal(err)
	}
	if got := surfaceOption(t, c, "@tern-tsp"); got != "external-value" {
		t.Fatal("unowned marker changed", got)
	}
	if err := c.ClaimSurface(ctx, "surface", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err := c.ReleaseStaleSurfaces(ctx); err != nil {
		t.Fatal(err)
	}
	if got := surfaceOption(t, c, "@tern-tsp"); got != "external-value" {
		t.Fatal("owned marker was not restored", got)
	}
}
