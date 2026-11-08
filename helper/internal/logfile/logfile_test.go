package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRotatesLargeLogsAndTightensMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(p, []byte(strings.Repeat("x", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Open(p, 1000)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 || st.Size() != 100 {
		t.Errorf("small log: mode %v size %d", st.Mode().Perm(), st.Size())
	}
	f, err = Open(p, 50)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("new\n")
	f.Close()
	if b, _ := os.ReadFile(p); string(b) != "new\n" {
		t.Errorf("after rotate: %q", b)
	}
	if st, err := os.Stat(p + ".1"); err != nil || st.Size() != 100 {
		t.Errorf("rotated copy: %v", err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("new log mode %v", st.Mode().Perm())
	}
}

func TestAdoptLeavesOtherOutputAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(p, []byte(strings.Repeat("x", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Adopt(p, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".1"); err == nil {
		t.Error("rotated a log that is not this process's stdout")
	}
}
