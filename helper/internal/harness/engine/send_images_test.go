package engine

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// A prompt with pictures pastes each path on its own, then the text, and
// its user item lists the pictures.
func TestSendPastesEachImagePathThenTheText(t *testing.T) {
	insertGap = 0
	pane := &seqPane{}
	var status atomic.Value
	status.Store("idle")
	e := statusEngine(t, "s_imagesend", pane, &status)
	r, err := e.Send(context.Background(), harness.SendRequest{
		Session: "s_imagesend", Text: "what changed?", Mode: harness.SendNow,
		Images: []string{"/u/one.png", "/u/two shot.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/u/one.png ", `/u/two\ shot.jpg `, "what changed?"}
	pane.mu.Lock()
	got := append([]string(nil), pane.typed...)
	pane.mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("typed %q, want %q", got, want)
	}
	if u := r.Item.User; u == nil || u.Text != "what changed?" || len(u.Images) != 2 || u.Images[1].Path != "/u/two shot.jpg" {
		t.Errorf("item %+v", r.Item.User)
	}
}

// The agent's record of a prompt sent with pictures takes the phone item's
// place even though it holds placeholders and paths.
func TestAgentRecordOfAnImagePromptMatchesThePhoneItem(t *testing.T) {
	images := []harness.ImageRef{{Path: "/u/one.png", MediaType: "image/png"}}
	got := withoutImages("[Image #1] /u/one.png  what changed?", images)
	if got != "what changed?" {
		t.Errorf("got %q", got)
	}
}
