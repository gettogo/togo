package update_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/release"
	"github.com/gettogo/togo/helper/internal/update"
	"github.com/gettogo/togo/helper/internal/update/updatetest"
)

var (
	oldBinary = []byte("#!/bin/sh\necho old\n")
	newBinary = []byte("#!/bin/sh\necho new\n")
)

// installed writes the binary update replaces and returns its path.
func installed(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "togo")
	if err := os.WriteFile(exe, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

// untouched fails unless exe still holds the old binary and nothing else
// was left in its directory.
func untouched(t *testing.T, exe string) {
	t.Helper()
	data, err := os.ReadFile(exe)
	if err != nil || !bytes.Equal(data, oldBinary) {
		t.Errorf("binary = %q, %v; want the original bytes", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"togo"}) {
		t.Errorf("install directory holds %q, want only togo", names)
	}
}

func archiveName(v string) string { return update.ArchiveName(v, runtime.GOOS, runtime.GOARCH) }

func TestUpdateRefusesTamperedArchive(t *testing.T) {
	const v = "v1.0.1"
	cases := []struct {
		name   string
		tamper func(t *testing.T, s *updatetest.Server)
		want   error
	}{
		{"modified archive", func(t *testing.T, s *updatetest.Server) {
			a := bytes.Clone(s.Asset(v, archiveName(v)))
			a[len(a)/2] ^= 0xff
			s.Put(v, archiveName(v), a)
		}, update.ErrChecksum},
		{"truncated archive", func(t *testing.T, s *updatetest.Server) {
			a := s.Asset(v, archiveName(v))
			s.Put(v, archiveName(v), a[:len(a)-10])
		}, update.ErrChecksum},
		{"archive swapped for another build", func(t *testing.T, s *updatetest.Server) {
			s.Put(v, archiveName(v), updatetest.Archive(t, []byte("evil")))
		}, update.ErrChecksum},
		{"sums re-signed by another key", func(t *testing.T, s *updatetest.Server) {
			sums := updatetest.Sums(map[string][]byte{archiveName(v): updatetest.Archive(t, []byte("evil"))})
			pub, priv := updatetest.NewKey(t)
			s.Put(v, archiveName(v), updatetest.Archive(t, []byte("evil")))
			s.Put(v, release.SumsName, sums)
			s.Put(v, release.SumsName+".minisig", updatetest.Sign(pub, priv, sums, v))
		}, release.ErrWrongKey},
		{"sums edited after signing", func(t *testing.T, s *updatetest.Server) {
			evil := updatetest.Archive(t, []byte("evil"))
			s.Put(v, archiveName(v), evil)
			s.Put(v, release.SumsName, updatetest.Sums(map[string][]byte{archiveName(v): evil}))
		}, release.ErrBadSignature},
		{"older release served as this one", func(t *testing.T, s *updatetest.Server) {
			// A genuine, correctly signed v1.0.0 whose files are renamed
			// to v1.0.1's: only the trusted comment tells them apart.
			old := updatetest.Archive(t, []byte("old release"))
			sums := updatetest.Sums(map[string][]byte{archiveName(v): old})
			s.Put(v, archiveName(v), old)
			s.Put(v, release.SumsName, sums)
			s.Resign(t, v, "v1.0.0")
		}, release.ErrWrongVersion},
		{"no archive for this platform", func(t *testing.T, s *updatetest.Server) {
			s.Put(v, release.SumsName, updatetest.Sums(map[string][]byte{"togo_1.0.1_plan9_mips.tar.gz": []byte("x")}))
			s.Resign(t, v, v)
		}, update.ErrMissingPlatform},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := updatetest.NewServer(t)
			s.Publish(t, v, newBinary)
			tc.tamper(t, s)
			exe := installed(t)
			err := update.Apply(context.Background(), s.Source(), v, exe)
			if err == nil {
				t.Fatal("Apply succeeded")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			untouched(t, exe)
		})
	}
}

func TestUpdateRefusesPlainHTTP(t *testing.T) {
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.1", newBinary)
	src := s.Source()
	src.BaseURL = "http://" + s.Listener.Addr().String() + "/releases"
	exe := installed(t)
	if err := update.Apply(context.Background(), src, "v1.0.1", exe); !errors.Is(err, update.ErrNotHTTPS) {
		t.Errorf("Apply over http: %v, want ErrNotHTTPS", err)
	}
	if _, _, err := update.Check(context.Background(), src, "v1.0.0"); !errors.Is(err, update.ErrNotHTTPS) {
		t.Errorf("Check over http: %v, want ErrNotHTTPS", err)
	}
	untouched(t, exe)
}

func TestUpdateRefusesBadVersion(t *testing.T) {
	s := updatetest.NewServer(t)
	exe := installed(t)
	for _, v := range []string{"", "1.0.1", "v1.0", "v1.0.1/../../x", "v1.0.1+meta", "latest"} {
		if err := update.Apply(context.Background(), s.Source(), v, exe); !errors.Is(err, update.ErrBadVersion) {
			t.Errorf("Apply(%q) = %v, want ErrBadVersion", v, err)
		}
	}
	untouched(t, exe)
}

func TestUpdateRefusesOlderVersion(t *testing.T) {
	s := updatetest.NewServer(t)
	s.Publish(t, "v1.0.0", oldBinary)
	latest, newer, err := update.Check(context.Background(), s.Source(), "v1.0.1")
	if err != nil || latest != "v1.0.0" || newer {
		t.Errorf("Check = %q, %v, %v; want v1.0.0 and not newer", latest, newer, err)
	}
	if _, newer, _ := update.Check(context.Background(), s.Source(), "v1.0.0"); newer {
		t.Error("the installed version counts as newer than itself")
	}
	if latest, newer, err := update.Check(context.Background(), s.Source(), "dev"); err != nil || latest != "v1.0.0" || !newer {
		t.Errorf("Check from dev = %q, %v, %v; want v1.0.0 newer", latest, newer, err)
	}
	order := []string{"dev", "v0.9.9", "v1.0.0-rc.1", "v1.0.0-rc.2", "v1.0.0-rc.10", "v1.0.0", "v1.0.1", "v1.2.0", "v1.10.0", "v2.0.0"}
	for i := range order {
		for j := range order {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := update.Compare(order[i], order[j]); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
}

func TestUpdateReplacesAtomically(t *testing.T) {
	const v = "v1.0.1"
	t.Run("success", func(t *testing.T) {
		s := updatetest.NewServer(t)
		s.Publish(t, v, newBinary)
		exe := installed(t)
		if err := update.Apply(context.Background(), s.Source(), v, exe); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(exe)
		if !bytes.Equal(data, newBinary) {
			t.Errorf("binary = %q, want the release", data)
		}
		info, err := os.Stat(exe)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("mode = %v, %v; want 0755", info.Mode().Perm(), err)
		}
		prev, err := os.ReadFile(exe + update.PreviousSuffix)
		if err != nil || !bytes.Equal(prev, oldBinary) {
			t.Errorf("previous = %q, %v; want the old binary kept", prev, err)
		}
		entries, _ := os.ReadDir(filepath.Dir(exe))
		if len(entries) != 2 {
			t.Errorf("install directory holds %d entries, want togo and togo.previous", len(entries))
		}
	})
	t.Run("failure mid-write", func(t *testing.T) {
		s := updatetest.NewServer(t)
		s.Publish(t, v, newBinary)
		// A correctly signed archive whose togo entry ends early: the
		// binary is half written when extraction fails.
		archive := updatetest.Archive(t, bytes.Repeat([]byte("x"), 1<<20))
		archive = truncatedGzip(t, archive)
		name := archiveName(v)
		sums := updatetest.Sums(map[string][]byte{name: archive})
		s.Put(v, name, archive)
		s.Put(v, release.SumsName, sums)
		s.Resign(t, v, v)
		exe := installed(t)
		err := update.Apply(context.Background(), s.Source(), v, exe)
		if err == nil {
			t.Fatal("Apply succeeded with a broken archive")
		}
		untouched(t, exe)
	})
}

// truncatedGzip cuts a gzip stream in the middle so it decodes part of the
// data and then fails.
func truncatedGzip(t *testing.T, gz []byte) []byte {
	t.Helper()
	return gz[:len(gz)/2]
}
