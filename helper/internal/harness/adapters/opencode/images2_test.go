package opencode

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// promptPayload is the part of session.inbox.enqueued that says what the
// terminal sent: the text and the attached files.
type promptPayload struct {
	Data struct {
		Item struct {
			Payload struct {
				Text  string `json:"text"`
				Files []struct {
					Name string `json:"name"`
					Mime string `json:"mime"`
				} `json:"files"`
			} `json:"payload"`
		} `json:"item"`
	} `json:"data"`
}

// Seen live on 2.3.0: a picture sent from the phone reached OpenCode as a
// file path in the prompt text, with no attachment. The terminal attaches a
// pasted file only when the paste is nothing but file paths
// (resolvePastedAttachments in its prompt component); the paths and the
// text pasted at once are plain text. So each path is pasted on its own.
//
// testdata/v2/image-prompts.sse holds what a 2.2.5 server recorded for the
// same two pictures and text typed both ways (folder renamed): first each
// path pasted alone and then the text, second all of it in one paste.
func TestPicturesArePastedOneByOneSoTheTerminalAttachesThem(t *testing.T) {
	recs := recordedSSE(t, "testdata/v2/image-prompts.sse")
	if len(recs) != 2 {
		t.Fatalf("recording holds %d prompts", len(recs))
	}
	var alone, atOnce promptPayload
	for i, p := range []*promptPayload{&alone, &atOnce} {
		if err := json.Unmarshal(recs[i].Data, p); err != nil {
			t.Fatal(err)
		}
	}
	got := alone.Data.Item.Payload
	if len(got.Files) != 2 || got.Files[0].Name != "red square.png" || got.Files[1].Mime != "image/png" ||
		got.Text != "[Image 1] [Image 2] what colour is this? one word" {
		t.Fatalf("paths pasted alone: %+v", got)
	}
	if got := atOnce.Data.Item.Payload; len(got.Files) != 0 {
		t.Fatalf("one paste attached %+v", got.Files)
	}

	a, err := spec.Parse(AdapterFile2())
	if err != nil {
		t.Fatal(err)
	}
	if a.Images == nil || a.Images.Insert != harness.InsertPerPath {
		t.Fatalf("pictures are pasted %+v, want one path per paste", a.Images)
	}
	// What is typed for those two pictures: a path with a space stays one
	// word, which the terminal unescapes before it reads the file.
	steps := harness.Insertions("what colour is this? one word", []string{"/tmp/work/red square.png", "/tmp/work/two.png"}, a.Images.Insert)
	want := []harness.Insertion{{Text: `/tmp/work/red\ square.png `}, {Text: "/tmp/work/two.png "}, {Text: "what colour is this? one word", Submit: true}}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("typed %+v", steps)
	}
}
