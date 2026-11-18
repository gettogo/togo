package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// OpenCode 2 writes an attached picture into the prompt as "[Image 1]",
// without Claude Code's "#" (recorded on 2.2.5: "[Image 1] [Image 2] what
// colour is this? one word"). Its record of the prompt must still take the
// phone item's place, or the chat shows the prompt twice.
func TestAgentRecordWithNumberedImagesMatchesThePhoneItem(t *testing.T) {
	images := []harness.ImageRef{{Path: "/tmp/work/red square.png", MediaType: "image/png"}, {Path: "/tmp/work/two.png", MediaType: "image/png"}}
	if got := withoutImages("[Image 1] [Image 2] what colour is this? one word", images); got != "what colour is this? one word" {
		t.Errorf("got %q", got)
	}
	// Ordinary text in brackets stays.
	for _, text := range []string{"see [Image] and [Images 1]", "arr[Image1] = [1]"} {
		if got := withoutImages(text, images); got != text {
			t.Errorf("%q became %q", text, got)
		}
	}
}
