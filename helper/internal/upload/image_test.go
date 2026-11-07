package upload_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/upload"
)

func pngBytes(t *testing.T, w, h int, noisy bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewPCG(1, 2))
	for y := range h {
		for x := range w {
			c := color.RGBA{uint8(x), uint8(y), 90, 255}
			if noisy {
				c = color.RGBA{uint8(r.IntN(256)), uint8(r.IntN(256)), uint8(r.IntN(256)), 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// imageService has a session whose working folder is cwd.
func imageService(t *testing.T) (*upload.Service, string) {
	t.Helper()
	cwd, _ := filepath.EvalSymlinks(t.TempDir())
	home, _ := filepath.EvalSymlinks(t.TempDir())
	return &upload.Service{
		Home:     home,
		Sessions: fakeSessions{managedID: {ID: managedID, Managed: true, Cwd: cwd}},
	}, cwd
}

func fetch(t *testing.T, s *upload.Service, path string) (upload.ImageResult, error) {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"sessionId": managedID, "path": path})
	got, err := s.Methods()["fs.image"](context.Background(), raw)
	if err != nil {
		return upload.ImageResult{}, err
	}
	return got.(upload.ImageResult), nil
}

func TestImageReadsAnUploadAndAFileInTheSessionFolder(t *testing.T) {
	s, cwd := imageService(t)
	data := pngBytes(t, 8, 8, false)
	up, err := call(t, s, params("shot.png", data))
	if err != nil {
		t.Fatal(err)
	}
	inCwd := filepath.Join(cwd, "docs", "a.png")
	os.MkdirAll(filepath.Dir(inCwd), 0o755)
	os.WriteFile(inCwd, data, 0o600)
	for _, p := range []string{up, inCwd} {
		r, err := fetch(t, s, p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		got, _ := base64.RawURLEncoding.DecodeString(r.Data)
		if r.MediaType != "image/png" || !bytes.Equal(got, data) || r.Scaled {
			t.Errorf("%s: %+v", p, r)
		}
	}
}

func TestImageRefusesPathsOutsideTheSessionFolders(t *testing.T) {
	s, cwd := imageService(t)
	outside := filepath.Join(t.TempDir(), "secret.png")
	os.WriteFile(outside, pngBytes(t, 4, 4, false), 0o600)
	link := filepath.Join(cwd, "link.png")
	os.Symlink(outside, link)
	other := filepath.Join(s.Home, "uploads", "s_otherother", "x.png")
	os.MkdirAll(filepath.Dir(other), 0o700)
	os.WriteFile(other, pngBytes(t, 4, 4, false), 0o600)
	for _, p := range []string{
		outside, link, other,
		cwd + "/../" + filepath.Base(outside), // not clean
		"docs/a.png",                          // relative
		"",
	} {
		if _, err := fetch(t, s, p); err == nil {
			t.Errorf("%q was read", p)
		}
	}
}

func TestImageRefusesFilesThatAreNotImages(t *testing.T) {
	s, cwd := imageService(t)
	notes := filepath.Join(cwd, "notes.txt")
	os.WriteFile(notes, []byte("hello"), 0o600)
	fake := filepath.Join(cwd, "fake.png")
	os.WriteFile(fake, []byte("#!/bin/sh\necho hi\n"), 0o600)
	for _, p := range []string{notes, fake} {
		if _, err := fetch(t, s, p); err == nil {
			t.Errorf("%s was read", p)
		}
	}
}

func TestImageShrinksALargePicture(t *testing.T) {
	s, cwd := imageService(t)
	big := pngBytes(t, 900, 700, true)
	if len(big) <= upload.MaxBytes {
		t.Fatalf("test picture only %d bytes", len(big))
	}
	p := filepath.Join(cwd, "big.png")
	os.WriteFile(p, big, 0o600)
	r, err := fetch(t, s, p)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := base64.RawURLEncoding.DecodeString(r.Data)
	if !r.Scaled || r.MediaType != "image/jpeg" || len(got) > upload.MaxBytes || r.Bytes != int64(len(big)) {
		t.Errorf("scaled %v type %s size %d bytes %d", r.Scaled, r.MediaType, len(got), r.Bytes)
	}
}

func TestImageUnmanagedSession(t *testing.T) {
	s, _ := imageService(t)
	raw, _ := json.Marshal(map[string]string{"sessionId": "u_6d7920776f726b", "path": "/a.png"})
	if _, err := s.Methods()["fs.image"](context.Background(), raw); err != session.ErrNotManaged {
		t.Errorf("err = %v", err)
	}
}
