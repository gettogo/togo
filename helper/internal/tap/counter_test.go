package tap

import (
	"testing"
	"time"
)

func TestCounterWritesTotalsAndEpoch(t *testing.T) {
	home := t.TempDir()
	c, err := NewCounter(home, "s_abcdefghij")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	c.now = func() time.Time { return now }
	c.written = now.Add(-time.Second)
	epoch, total, err := ReadOffset(home, "s_abcdefghij")
	if err != nil || total != 0 || epoch != c.Epoch() || len(epoch) != 16 {
		t.Fatalf("first read = %q %d %v", epoch, total, err)
	}
	c.Add(10) // written: a second since the last write
	now = now.Add(10 * time.Millisecond)
	c.Add(5) // throttled
	if _, total, _ := ReadOffset(home, "s_abcdefghij"); total != 10 {
		t.Fatalf("throttled total = %d, want 10", total)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := ReadOffset(home, "s_abcdefghij"); total != 15 {
		t.Fatalf("closed total = %d, want 15", total)
	}
	d, err := NewCounter(home, "s_abcdefghij")
	if err != nil {
		t.Fatal(err)
	}
	if d.Epoch() == c.Epoch() {
		t.Error("a new tap must start a new epoch")
	}
	if _, err := OffsetPath(home, "../x"); err == nil {
		t.Error("bad id accepted")
	}
}
