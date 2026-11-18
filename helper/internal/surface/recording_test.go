package surface

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Captured from OMP 18.20.0: open, resume picker, scope change, close and
// quit. Only the session file path was replaced with a fixture path.
func TestRecordedOMPSessionAppliesWithoutLosingPickerClose(t *testing.T) {
	f, err := os.Open("testdata/omp-session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var p Parser
	var doc *Document
	frames := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, MaxAPCBytes)
	for sc.Scan() {
		var record struct {
			Verb string          `json:"verb"`
			Body json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(sc.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		msgs, errs := p.Feed(apc(record.Verb+";"+string(record.Body)), time.Time{})
		if len(errs) != 0 || len(msgs) != 1 {
			t.Fatalf("record failed parsing: %v", errs)
		}
		switch record.Verb {
		case "o":
			doc, err = NewDocument("s:1", []string{"card", "col", "editor", "icon", "image", "item", "kbd", "list", "meter", "overlay", "picker", "row", "spacer", "status", "text", "toast"})
			if err != nil {
				t.Fatal(err)
			}
		case "f":
			bad, err := doc.Apply(msgs[0].Body)
			if err != nil || len(bad) != 0 {
				t.Fatalf("frame %d: %v %v", frames+1, bad, err)
			}
			frames++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if frames != 7 || doc.Sequence != 7 || doc.nodes["o"] != nil || doc.Focus != "l.line/input" {
		t.Fatalf("record lost state: frames=%d sequence=%d focus=%q", frames, doc.Sequence, doc.Focus)
	}
	if doc.nodes["l.line/input"].node.Props["text"] != "" {
		t.Fatal("quit command remained in the composer")
	}
}

func FuzzParserKeepsFragmentBuffersBounded(f *testing.F) {
	f.Add(apc(`q;{"q":"hello","v":[1]}`))
	f.Add([]byte("\x1b_tsp;f;c=a;m=1;{\x1b\\\x1b]2;title\x07"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxMessageBytes {
			t.Skip()
		}
		var p Parser
		for at := 0; at < len(data); at += 31 {
			end := min(at+31, len(data))
			p.Feed(data[at:end], time.Time{})
			if len(p.body) > MaxStringBytes || len(p.chunks.body) > MaxMessageBytes {
				t.Fatal("fragment buffer exceeded its byte budget")
			}
		}
	})
}
