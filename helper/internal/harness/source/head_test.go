package source

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

const (
	titleLine   = `{"type":"title","title":"Fix the parser"}`
	sessionLine = `{"type":"session","id":"abc"}`
)

func replayWith(t *testing.T, body []byte, typ string) [][]byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	var got []harness.Record
	tl := &tail{path: path, name: "session", header: typ, replay: true}
	if err := tl.poll(func(r harness.Record) { got = append(got, r) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("deliveries = %d", len(got))
	}
	out := make([][]byte, len(got[0].Replay))
	for i, r := range got[0].Replay {
		out[i] = r
	}
	return out
}

// titled is a file that opens with a title record, then its session
// record, then n messages (each padded by pad).
func titled(n int, pad string) []byte {
	b := []byte(titleLine + "\n" + sessionLine + "\n")
	for i := 0; i < n; i++ {
		b = append(b, `{"type":"message","n":`+strconv.Itoa(i)+pad+"}\n"...)
	}
	return b
}

// A file whose session record is its second line keeps both leading
// records when the replay is cut by count, once each, within the limit.
func TestReplayKeepsLeadingRecordsUpToTheHeaderByCount(t *testing.T) {
	recs := replayWith(t, titled(MaxRecords+50, ""), "session")
	if len(recs) != MaxRecords || string(recs[0]) != titleLine || string(recs[1]) != sessionLine {
		t.Fatalf("len=%d first=%s second=%s", len(recs), recs[0], recs[1])
	}
	if strings.Contains(string(recs[2]), `"session"`) || string(recs[len(recs)-1]) != `{"type":"message","n":`+strconv.Itoa(MaxRecords+49)+"}" {
		t.Fatalf("third=%s last=%s", recs[2], recs[len(recs)-1])
	}
	// One record past the limit: the tail starts after the header.
	recs = replayWith(t, titled(MaxRecords-1, ""), "session")
	if len(recs) != MaxRecords || string(recs[1]) != sessionLine || string(recs[2]) != `{"type":"message","n":1}` {
		t.Fatalf("len=%d second=%s third=%s", len(recs), recs[1], recs[2])
	}
}

// The same when the file is larger than one read.
func TestReplayKeepsLeadingRecordsUpToTheHeaderByBytes(t *testing.T) {
	recs := replayWith(t, titled(30, `,"pad":"`+strings.Repeat("x", 400<<10)+`"`), "session")
	if len(recs) < 3 || string(recs[0]) != titleLine || string(recs[1]) != sessionLine {
		t.Fatalf("len=%d first=%.60s second=%.60s", len(recs), recs[0], recs[1])
	}
}

// Without a header type, or with one not among the first records, only
// the first record is kept, as for every other file.
func TestReplayKeepsOnlyTheFirstRecordOtherwise(t *testing.T) {
	for _, typ := range []string{"", "nothing-like-this"} {
		recs := replayWith(t, titled(MaxRecords+50, ""), typ)
		if len(recs) != MaxRecords || string(recs[0]) != titleLine || string(recs[1]) == sessionLine {
			t.Fatalf("%q: len=%d first=%s second=%s", typ, len(recs), recs[0], recs[1])
		}
	}
}
