package surface

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
)

func TestCompanionVersionGateLeavesOlderAndUnknownLaunchesAlone(t *testing.T) {
	for _, output := range []string{"omp/18.16.0", "omp/18.19.9", "omp/19.0.0", "unexpected version", "omp/18.20.0 trailing text"} {
		t.Run(output, func(t *testing.T) {
			c := testCompanions(t)
			c.probeVersion = func(context.Context, session.Launch) (string, error) { return output, nil }
			old := session.Launch{Program: "omp", Args: []string{"--profile", "work"}}
			got, err := c.PrepareLaunch(context.Background(), "s_abcdefghij", "omp", old)
			if err != nil || !reflect.DeepEqual(got, old) {
				t.Fatal("unsupported launch changed", err)
			}
			entries, err := os.ReadDir(c.Home)
			if err != nil || len(entries) != 0 {
				t.Fatal("unsupported launch wrote integration files", err)
			}
		})
	}
	for _, output := range []string{"omp/18.20.0\n", "omp/18.20.1", "omp/18.21.0", "omp/18.20.0-beta.1"} {
		if !supportedOMPVersion(output) {
			t.Fatal("supported CLI version rejected", output)
		}
	}
}

func TestCompanionProbeFailurePreservesTheOriginalLaunch(t *testing.T) {
	c := testCompanions(t)
	c.probeVersion = func(context.Context, session.Launch) (string, error) { return "", context.DeadlineExceeded }
	old := session.Launch{Program: "omp", Args: []string{"--profile", "work"}}
	got, err := c.PrepareLaunch(context.Background(), "s_abcdefghij", "omp", old)
	if !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(got, old) {
		t.Fatal("optional probe changed launch", err)
	}
}

func TestPerRunNativeOptOutDoesNotProbeOrWrite(t *testing.T) {
	c := testCompanions(t)
	c.probeVersion = func(context.Context, session.Launch) (string, error) {
		t.Fatal("disabled launch probed runtime")
		return "", nil
	}
	old := session.Launch{Program: "omp", Env: map[string]string{"PI_TUI_NATIVE": "0"}}
	got, err := c.PrepareLaunch(context.Background(), "s_abcdefghij", "omp", old)
	if err != nil || !reflect.DeepEqual(got, old) {
		t.Fatal("per-run opt-out changed launch", err)
	}
}
