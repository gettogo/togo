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
	var proof []appinventory.Proof
	var revision string
	if path := os.Getenv("TOGO_APP_DEBT_PROOF"); path != "" {
		revision = os.Getenv("TOGO_APP_DEBT_REVISION")
		if revision == "" {
			t.Fatal("proof requires the candidate revision")
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		proof, err = appinventory.DecodeProof(file)
		if err != nil {
			t.Fatalf("read app proof: %v", err)
		}
	}
	if err := appinventory.CheckRatchet(keys, debt, base, proof, revision); err != nil {
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
