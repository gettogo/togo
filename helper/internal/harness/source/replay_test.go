package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func replayOf(t *testing.T, body []byte) []json.RawMessage {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	var got []harness.Record
	tl := &tail{path: path, name: "session", replay: true}
	if err := tl.poll(func(r harness.Record) { got = append(got, r) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("deliveries = %d", len(got))
	}
	return got[0].Replay
}

// A long session keeps its header line when the replay is cut to the
// last records.
func TestReplayKeepsHeaderWhenCutByCount(t *testing.T) {
	var b []byte
	b = append(b, `{"type":"session","id":"abc"}`+"\n"...)
	for i := 0; i < MaxRecords+50; i++ {
		b = append(b, `{"type":"message","n":`+strconv.Itoa(i)+"}\n"...)
	}
	recs := replayOf(t, b)
	if len(recs) != MaxRecords || string(recs[0]) != `{"type":"session","id":"abc"}` {
		t.Fatalf("len=%d first=%s", len(recs), recs[0])
	}
	if string(recs[len(recs)-1]) != `{"type":"message","n":`+strconv.Itoa(MaxRecords+49)+"}" {
		t.Fatalf("last = %s", recs[len(recs)-1])
	}
}

// The same when the file is larger than one read (pasted images).
func TestReplayKeepsHeaderWhenCutByBytes(t *testing.T) {
	pad := strings.Repeat("x", 400<<10)
	var b []byte
	b = append(b, `{"type":"session","id":"abc"}`+"\n"...)
	for i := 0; i < 30; i++ {
		b = append(b, `{"type":"message","n":`+strconv.Itoa(i)+`,"pad":"`+pad+"\"}\n"...)
	}
	recs := replayOf(t, b)
	if len(recs) < 2 || string(recs[0]) != `{"type":"session","id":"abc"}` {
		t.Fatalf("len=%d first=%.60s", len(recs), recs[0])
	}
	if !json.Valid(recs[1]) {
		t.Fatal("partial line replayed")
	}
}
