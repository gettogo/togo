package harness

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestImageAtPath(t *testing.T) {
	if r, ok := ImageAtPath("/tmp/a/../shot.PNG"); !ok || r.Path != "/tmp/shot.PNG" || r.MediaType != "image/png" {
		t.Errorf("got %+v %v", r, ok)
	}
	for _, p := range []string{"rel/shot.png", "/tmp/notes.txt", ""} {
		if _, ok := ImageAtPath(p); ok {
			t.Errorf("%q accepted", p)
		}
	}
}

func TestInlineImageCapsData(t *testing.T) {
	small := base64.StdEncoding.EncodeToString([]byte("png"))
	r, ok := InlineImage("image/png", small)
	if !ok || r.Data != base64.RawURLEncoding.EncodeToString([]byte("png")) || r.Bytes != 3 {
		t.Errorf("small: %+v", r)
	}
	big := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", MaxInlineImage+1)))
	r, ok = InlineImage("image/jpeg", big)
	if !ok || r.Data != "" || r.Bytes != MaxInlineImage+1 {
		t.Errorf("big carried data or lost size: bytes %d data %d", r.Bytes, len(r.Data))
	}
	if _, ok := InlineImage("text/plain", small); ok {
		t.Error("non-image accepted")
	}
	if _, ok := InlineImage("image/png", "%%%"); ok {
		t.Error("bad base64 accepted")
	}
}
