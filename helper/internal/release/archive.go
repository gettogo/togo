package release

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ArchiveName is the file name of the archive for t.
func (c Config) ArchiveName(t Target) string {
	return fmt.Sprintf("togo_%s_%s_%s.tar.gz", strings.TrimPrefix(c.Version, "v"), t.OS, t.Arch)
}

// Archive writes the release archive for t into OutDir. It holds the binary
// as togo and the license as LICENSE, with fixed modes, owners and times, so
// equal inputs give equal bytes.
func Archive(c Config, t Target, binPath, licensePath string) (string, error) {
	if err := os.MkdirAll(c.OutDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(c.OutDir, c.ArchiveName(t))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if err := writeArchive(f, c.SourceDateEpoch, []entry{
		{"LICENSE", licensePath, 0o644},
		{"togo", binPath, 0o755},
	}); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

type entry struct {
	name, src string
	mode      int64
}

// writeArchive expects entries already sorted by name.
func writeArchive(f *os.File, epoch int64, entries []entry) error {
	zw := gzip.NewWriter(f)
	zw.Header = gzip.Header{}
	tw := tar.NewWriter(zw)
	mtime := time.Unix(epoch, 0).UTC()
	for _, e := range entries {
		data, err := os.ReadFile(e.src)
		if err != nil {
			return err
		}
		h := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     e.name,
			Size:     int64(len(data)),
			Mode:     e.mode,
			ModTime:  mtime,
			Format:   tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}
