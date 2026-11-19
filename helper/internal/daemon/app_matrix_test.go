package daemon

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/appinventory"
)

func appMatrixKeys(t *testing.T) []string {
	t.Helper()
	inv, err := appinventory.Read(agents.Registry, harness.AdapterFiles())
	if err != nil {
		t.Fatal(err)
	}
	keys, err := appinventory.MatrixKeys(inv)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestAppMatrixDebt(t *testing.T) {
	keys := appMatrixKeys(t)
	debt := readAppDebt(t, "testdata/app_missing")
	base := debt
	if path := os.Getenv("TOGO_APP_DEBT_BASE"); path != "" {
		base = readAppDebt(t, path)
	}
	// No app cells have passing execution evidence yet. A real UI
	// results reader must supply it before any listed cell can be retired.
	if err := appinventory.CheckRatchet(keys, debt, base, nil, ""); err != nil {
		t.Fatalf("app matrix debt (%d cells): %v", len(keys), err)
	}
}

func readAppDebt(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			t.Fatalf("unexpected app debt file %s", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.TrimSuffix(string(data), "\n")
		if lines == "" {
			continue
		}
		shard := strings.Split(lines, "\n")
		if !slices.IsSorted(shard) {
			t.Fatalf("app debt file %s must be sorted", entry.Name())
		}
		keys = append(keys, shard...)
	}
	slices.Sort(keys)
	return keys
}
