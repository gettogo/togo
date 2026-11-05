package upload_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/upload"
)

const managedID = "s_abcdefghij"

type fakeSessions map[string]session.Session

func (f fakeSessions) Resolve(_ context.Context, id string) (session.Session, error) {
	if s, ok := f[id]; ok {
		return s, nil
	}
	return session.Session{}, session.ErrNotFound
}

var now = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

func newService(t *testing.T) *upload.Service {
	t.Helper()
	return &upload.Service{
		Home: t.TempDir(),
		Sessions: fakeSessions{
			managedID:          {ID: managedID, Managed: true},
			"u_6d7920776f726b": {ID: "u_6d7920776f726b", Managed: false},
		},
		Now: func() time.Time { return now },
	}
}

func call(t *testing.T, s *upload.Service, params map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Methods()["fs.upload"](context.Background(), raw)
	if err != nil {
		return "", err
	}
	return got.(map[string]string)["path"], nil
}

func params(name string, data []byte) map[string]any {
	return map[string]any{
		"sessionId": managedID, "name": name,
		"data": base64.RawURLEncoding.EncodeToString(data),
	}
}

func TestUploadWritesPrivateFile(t *testing.T) {
	s := newService(t)
	path, err := call(t, s, params("shot.png", []byte{0xfb, 0xff, 0x00, 0x01}))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	wantDir := filepath.Join(s.Home, "uploads", managedID)
	if filepath.Dir(path) != wantDir {
		t.Errorf("path %s not in %s", path, wantDir)
	}
	if want := "1793782800000-shot.png"; filepath.Base(path) != want {
		t.Errorf("file name = %s, want %s", filepath.Base(path), want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}
	if dir, _ := os.Stat(wantDir); dir.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v, want 0700", dir.Mode().Perm())
	}
	if got, _ := os.ReadFile(path); string(got) != "\xfb\xff\x00\x01" {
		t.Errorf("contents = %x", got)
	}
}

func TestUploadSameNameSameMillisecondKeepsBoth(t *testing.T) {
	s := newService(t)
	a, errA := call(t, s, params("a.txt", []byte("1")))
	b, errB := call(t, s, params("a.txt", []byte("2")))
	if errA != nil || errB != nil || a == b {
		t.Fatalf("paths %q %q, errors %v %v; want two distinct files", a, b, errA, errB)
	}
}

func TestUploadRejectsTraversalName(t *testing.T) {
	s := newService(t)
	for name, want := range map[string]string{
		"../../x":       "x",
		`..\..\win.txt`: "win.txt",
		"/etc/passwd":   "passwd",
		"..":            "upload",
		".hidden":       "hidden",
		"a b$(id).png":  "a_b__id_.png",
		"":              "upload",
		"dir/":          "upload",
		"shoté.png":     "shot_.png",
	} {
		path, err := call(t, s, params(name, []byte("d")))
		if err != nil {
			t.Fatalf("upload %q: %v", name, err)
		}
		if got := strings.SplitN(filepath.Base(path), "-", 2)[1]; got != want {
			t.Errorf("name %q stored as %q, want %q", name, got, want)
		}
		rel, err := filepath.Rel(filepath.Join(s.Home, "uploads", managedID), path)
		if err != nil || !filepath.IsLocal(rel) || strings.Contains(rel, string(filepath.Separator)) {
			t.Errorf("name %q escaped to %s", name, path)
		}
	}
}

func TestSanitizeNameLimitsLengthAndKeepsExtension(t *testing.T) {
	got := upload.SanitizeName(strings.Repeat("a", 200) + ".jpeg")
	if len(got) != 64 || !strings.HasSuffix(got, ".jpeg") {
		t.Errorf("got %q (%d bytes), want 64 bytes ending in .jpeg", got, len(got))
	}
	long := upload.SanitizeName("x." + strings.Repeat("e", 100))
	if len(long) > 64 {
		t.Errorf("long extension gave %d bytes", len(long))
	}
}

func TestUploadRejectsBadSessionIDs(t *testing.T) {
	s := newService(t)
	for _, id := range []string{"../s_abcdefghij", "s_abc/../x", "..", "", "s_abcdefghij/", "4"} {
		p := params("a.txt", []byte("d"))
		p["sessionId"] = id
		_, err := call(t, s, p)
		var invalid *session.InvalidParamsError
		if !errors.As(err, &invalid) {
			t.Errorf("sessionId %q: error = %v, want invalid params", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Home, "uploads")); err == nil {
		t.Error("a rejected id created the uploads directory")
	}
}

func TestUploadTooLarge(t *testing.T) {
	s := newService(t)
	var invalid *session.InvalidParamsError
	if _, err := call(t, s, params("big.bin", make([]byte, upload.MaxBytes+1))); !errors.As(err, &invalid) {
		t.Errorf("oversize upload error = %v, want invalid params", err)
	}
	if _, err := call(t, s, params("max.bin", make([]byte, upload.MaxBytes))); err != nil {
		t.Errorf("upload of exactly the limit: %v", err)
	}
}

func TestUploadRejectsBadData(t *testing.T) {
	s := newService(t)
	p := params("a.txt", nil)
	p["data"] = "not base64!"
	var invalid *session.InvalidParamsError
	if _, err := call(t, s, p); !errors.As(err, &invalid) {
		t.Errorf("error = %v, want invalid params", err)
	}
	p["data"] = "AAAA="
	if _, err := call(t, s, p); !errors.As(err, &invalid) {
		t.Errorf("padded data error = %v, want invalid params", err)
	}
}

func TestUploadRejectsNonCanonicalData(t *testing.T) {
	s := newService(t)
	var invalid *session.InvalidParamsError
	// "YR" decodes to 'a' only if the unused low bits are ignored; "YQ" is canonical.
	for name, data := range map[string]string{"trailing bits": "YR", "newline": "YQ\nYQ", "carriage return": "YQ\rYQ"} {
		p := params("a.txt", nil)
		p["data"] = data
		if _, err := call(t, s, p); !errors.As(err, &invalid) {
			t.Errorf("%s: error = %v, want invalid params", name, err)
		}
	}
}

func TestUploadRefusesSymlinkedDirectories(t *testing.T) {
	target := t.TempDir()
	s := newService(t)
	uploads := filepath.Join(s.Home, "uploads")
	if err := os.Mkdir(uploads, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(uploads, managedID)); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, s, params("a.txt", []byte("d"))); err == nil {
		t.Error("upload through a symlinked session directory succeeded")
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Errorf("symlink target received %d entries", len(entries))
	}

	s = newService(t)
	if err := os.Symlink(target, filepath.Join(s.Home, "uploads")); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, s, params("a.txt", []byte("d"))); err == nil {
		t.Error("upload through a symlinked uploads root succeeded")
	}
}

func TestUploadUnmanaged(t *testing.T) {
	s := newService(t)
	p := params("a.txt", []byte("d"))
	p["sessionId"] = "u_6d7920776f726b"
	if _, err := call(t, s, p); !errors.Is(err, session.ErrNotManaged) {
		t.Errorf("error = %v, want ErrNotManaged", err)
	}
}

func TestUploadUnknownSession(t *testing.T) {
	s := newService(t)
	p := params("a.txt", []byte("d"))
	p["sessionId"] = "s_zzzzzzzzzz"
	if _, err := call(t, s, p); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestSweepRemovesOld(t *testing.T) {
	s := newService(t)
	oldPath, err := call(t, s, params("old.txt", []byte("o")))
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return now.Add(time.Hour) }
	freshPath, err := call(t, s, params("fresh.txt", []byte("f")))
	if err != nil {
		t.Fatal(err)
	}
	lone, err := call(t, s, map[string]any{"sessionId": managedID, "name": "x", "data": ""})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldPath, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(lone, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(freshPath, now.Add(-6*24*time.Hour), now.Add(-6*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := s.Sweep(); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	for _, gone := range []string{oldPath, lone} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived the sweep (%v)", gone, err)
		}
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Errorf("fresh upload removed: %v", err)
	}
}

func TestSweepRemovesEmptySessionDirectory(t *testing.T) {
	s := newService(t)
	path, err := call(t, s, params("old.txt", []byte("o")))
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty session directory survived (%v)", err)
	}
}

func TestSweepWithoutUploadsDirectory(t *testing.T) {
	if err := newService(t).Sweep(); err != nil {
		t.Errorf("Sweep on a fresh home: %v", err)
	}
}
