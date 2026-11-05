package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func archiveInputs(t *testing.T, mtime time.Time) (bin, lic string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "togo")
	lic = filepath.Join(dir, "LICENSE")
	if err := os.WriteFile(bin, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lic, []byte("license"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{bin, lic} {
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	return bin, lic
}

func TestArchiveIsByteStable(t *testing.T) {
	c := Config{Version: "v1.2.3", SourceDateEpoch: 1700000000}
	tgt := Target{"linux", "amd64"}
	var outs [][]byte
	for i, mt := range []time.Time{time.Unix(1, 0), time.Unix(1900000000, 0)} {
		old := syscall.Umask([]int{0o022, 0o077}[i])
		bin, lic := archiveInputs(t, mt)
		c.OutDir = t.TempDir()
		path, err := Archive(c, tgt, bin, lic)
		syscall.Umask(old)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(path) != "togo_1.2.3_linux_amd64.tar.gz" {
			t.Errorf("name = %s", filepath.Base(path))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		outs = append(outs, data)
	}
	if !bytes.Equal(outs[0], outs[1]) {
		t.Error("archives differ")
	}
}

func TestArchiveEntries(t *testing.T) {
	bin, lic := archiveInputs(t, time.Now())
	c := Config{Version: "v1.2.3", SourceDateEpoch: 1700000000, OutDir: t.TempDir()}
	path, err := Archive(c, Target{"darwin", "arm64"}, bin, lic)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if zr.Name != "" || !zr.ModTime.IsZero() {
		t.Errorf("gzip header carries name %q or mtime %v", zr.Name, zr.ModTime)
	}
	tr := tar.NewReader(zr)
	type ent struct {
		mode int64
		body string
	}
	got := map[string]ent{}
	var order []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		got[h.Name] = ent{h.Mode, string(b)}
		order = append(order, h.Name)
		if h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" {
			t.Errorf("%s owner = %d/%d %q/%q", h.Name, h.Uid, h.Gid, h.Uname, h.Gname)
		}
		if h.ModTime.Unix() != 1700000000 {
			t.Errorf("%s mtime = %v", h.Name, h.ModTime)
		}
	}
	if len(order) != 2 || order[0] != "LICENSE" || order[1] != "togo" {
		t.Fatalf("entries = %v", order)
	}
	if got["togo"].mode != 0o755 || got["togo"].body != "binary" {
		t.Errorf("togo = %+v", got["togo"])
	}
	if got["LICENSE"].mode != 0o644 || got["LICENSE"].body != "license" {
		t.Errorf("LICENSE = %+v", got["LICENSE"])
	}
}
