package tmux

import (
	"bytes"
	"testing"
)

func TestControlOutputDecodesOctalWithoutTouchingOtherPanes(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []byte
		ok   bool
		bad  bool
	}{
		{`%output %1 \033_tsp;q;{"q":"hello"}\033\134`, []byte("\x1b_tsp;q;{\"q\":\"hello\"}\x1b\\"), true, false},
		{`%output %10 hello`, nil, false, false},
		{`%extended-output %1 0 : hello\015\012`, []byte("hello\r\n"), true, false},
		{`%output %1 \999`, nil, true, true},
		{`%output %1 trailing\`, nil, true, true},
	} {
		got, ok, err := controlOutput([]byte(tc.line), "%1")
		if ok != tc.ok || (err != nil) != tc.bad || !tc.bad && !bytes.Equal(got, tc.want) {
			t.Fatalf("line %q: got=%q ok=%v err=%v", tc.line, got, ok, err)
		}
	}
}
