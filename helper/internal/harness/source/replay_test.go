package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

func TestInitialTranscriptReplayIsOneDelivery(t *testing.T) {
	fixture, err := os.ReadFile("../adapters/omp/testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	tail := &tail{path: path, name: "session", replay: true}
	var delivered []harness.Record
	if err := tail.poll(func(record harness.Record) { delivered = append(delivered, record) }); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 1 {
		t.Fatalf("restored history arrived as %d separate live deliveries", len(delivered))
	}
	if len(delivered[0].Replay) != len(Records(fixture, 1<<30)) {
		t.Fatal("restoration did not retain the mapper records")
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("{\"live\":true}\n")
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := tail.poll(func(record harness.Record) { delivered = append(delivered, record) }); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 2 || delivered[1].Replay != nil || !json.Valid(delivered[1].Data) {
		t.Fatal("new output did not remain a live record")
	}
}
