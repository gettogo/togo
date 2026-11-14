package surface_test

import (
	"context"
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/surface"
)

func checkUnstartedReplacement(t *testing.T, ctx context.Context, companions *surface.Companions, id, binary, generation string) {
	t.Helper()
	_, err := companions.PrepareLaunch(ctx, id, "omp", session.Launch{Program: binary})
	if err != nil {
		t.Fatal("prepare an unstarted replacement", err)
	}
	current, err := companions.Current(id)
	if err != nil || current.Generation != generation {
		t.Fatal("unstarted replacement hid the original process", err)
	}
	t.Log("unstarted replacement retained the original runtime mapping")
}
