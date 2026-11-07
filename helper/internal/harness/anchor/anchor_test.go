package anchor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/tap"
)

// fakePane is a pane whose lines are a slice: the screen is the last
// height lines, history the rest.
type fakePane struct {
	lines  []string
	height int64
	limit  int64
	calls  int
}

func (f *fakePane) pos() Position {
	h := max(int64(len(f.lines))-f.height, 0)
	return Position{History: h, CursorY: int64(len(f.lines)) - 1 - h, Limit: f.limit}
}

func (f *fakePane) Position(context.Context, string) (Position, error) {
	f.calls++
	return f.pos(), nil
}

func (f *fakePane) Lines(_ context.Context, _ string, from, to int64) ([]string, error) {
	h := f.pos().History
	var out []string
	for i := from + h; i <= to+h; i++ {
		if i >= 0 && i < int64(len(f.lines)) {
			out = append(out, f.lines[i])
		}
	}
	return out, nil
}

func (f *fakePane) History(context.Context, string) ([]string, error) { return f.lines, nil }

func (f *fakePane) add(s ...string) { f.lines = append(f.lines, s...) }

const sid = "s_abcdefghij"

func TestMarkAndFind(t *testing.T) {
	home := t.TempDir()
	c, err := tap.NewCounter(home, sid)
	if err != nil {
		t.Fatal(err)
	}
	c.Add(1200)
	_ = c.Flush()
	pane := &fakePane{height: 5, limit: 1000}
	pane.add("$ claude", "", "> fix the failing date test")
	now := time.Unix(0, 0)
	m := &Marker{Home: home, Pane: pane, Now: func() time.Time { return now }}
	a, err := m.Mark(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if a.Epoch != c.Epoch() || a.Bytes != 1200 || a.Line != 2 || a.Needle != "> fix the failing date test" {
		t.Fatalf("mark = %+v", a)
	}
	// Cached within 250 ms.
	if _, err := m.Mark(context.Background(), sid); err != nil || pane.calls != 1 {
		t.Fatalf("position not cached: %d calls", pane.calls)
	}
	for i := 0; i < 30; i++ {
		pane.add("output line")
	}
	loc, err := m.Find(context.Background(), sid, a)
	if err != nil {
		t.Fatal(err)
	}
	if !loc.Found || loc.LinesFromBottom != 30 {
		t.Fatalf("find = %+v", loc)
	}
}

func TestFindSearchesNeedleWhenEpochChanged(t *testing.T) {
	home := t.TempDir()
	pane := &fakePane{height: 3}
	pane.add("a", "Bash(go test ./...)", "b", "c", "d")
	m := &Marker{Home: home, Pane: pane}
	loc, err := m.Find(context.Background(), sid, harness.Anchor{Epoch: "old", Line: 99, Needle: "Bash(go test ./...)"})
	if err != nil {
		t.Fatal(err)
	}
	if !loc.Found || loc.LinesFromBottom != 3 {
		t.Fatalf("find = %+v", loc)
	}
	loc, _ = m.Find(context.Background(), sid, harness.Anchor{Epoch: "old", Line: 1, Needle: "gone"})
	if loc.Found {
		t.Fatal("missing needle found")
	}
	loc, _ = m.Find(context.Background(), sid, harness.Anchor{Epoch: "old", Line: 1})
	if loc.Found {
		t.Fatal("no needle and another epoch must not be found")
	}
}

func TestFindRejectsWrongLineByNeedle(t *testing.T) {
	home := t.TempDir()
	pane := &fakePane{height: 3, limit: 4}
	pane.add("x1", "target", "x2", "x3", "x4", "x5", "x6")
	m := &Marker{Home: home, Pane: pane}
	// History is at its limit, so line numbers shifted: search instead.
	loc, err := m.Find(context.Background(), sid, harness.Anchor{Line: 0, Needle: "target"})
	if err != nil {
		t.Fatal(err)
	}
	if !loc.Found || loc.LinesFromBottom != 5 {
		t.Fatalf("find = %+v", loc)
	}
}

func TestParsePositionAndNeedle(t *testing.T) {
	p, err := ParsePosition("120 17 2000\n")
	if err != nil || p.Line() != 137 || p.Limit != 2000 {
		t.Fatalf("%+v %v", p, err)
	}
	for _, bad := range []string{"", "1 2", "a b c", "-1 0 0"} {
		if _, err := ParsePosition(bad); err == nil {
			t.Errorf("ParsePosition(%q) accepted", bad)
		}
	}
	long := strings.Repeat("é", 200)
	if n := needle([]string{long, "  "}); len([]rune(n)) != harness.MaxNeedle {
		t.Errorf("needle not trimmed: %d runes", len([]rune(n)))
	}
}
