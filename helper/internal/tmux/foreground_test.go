package tmux

import (
	"reflect"
	"testing"
	"time"
)

func TestParseForegroundKeepsForegroundGroup(t *testing.T) {
	out := "  100   100   300 01:02:03 -zsh\n  200   200   300 00:05 /usr/local/bin/gitstatusd\n  300   300   300 00:04 /opt/to go/togo\n  301   300   300 00:01 node\nbad line\n"
	want := []Proc{{PID: 300, Name: "togo"}, {PID: 301, Name: "node"}}
	got, age := parseForeground(out, 100)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if age != time.Hour+2*time.Minute+3*time.Second {
		t.Fatalf("pane age %v", age)
	}
}

func TestParseEtime(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"00:07":      7 * time.Second,
		"12:34":      12*time.Minute + 34*time.Second,
		"01:00:00":   time.Hour,
		"2-03:04:05": 51*time.Hour + 4*time.Minute + 5*time.Second,
		"":           0,
		"x:01":       0,
		"1:2:3:4":    0,
		"-1:00":      0,
	} {
		if got := parseEtime(in); got != want {
			t.Errorf("parseEtime(%q) = %v, want %v", in, got, want)
		}
	}
}
