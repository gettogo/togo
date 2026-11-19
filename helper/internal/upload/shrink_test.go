package upload

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"
)

// Only one picture is decoded at a time: a decoded 50 megapixel picture is
// hundreds of megabytes, and fs.image calls arrive concurrently.
func TestShrinkRunsOneAtATime(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 40, 40))); err != nil {
		t.Fatal(err)
	}
	shrinkSlots <- struct{}{}
	done := make(chan error, 1)
	go func() { _, err := shrink(buf.Bytes(), 1<<20); done <- err }()
	select {
	case <-done:
		t.Fatal("shrink ran while another one held the slot")
	case <-time.After(200 * time.Millisecond):
	}
	<-shrinkSlots
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("shrink did not run after the slot was free")
	}
}
