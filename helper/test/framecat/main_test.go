package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFramesRoundTripThroughEncodeAndDecode(t *testing.T) {
	lines := `{"id":1,"method":"hello"}` + "\n\n" + `{"id":2,"method":"session.list"}` + "\n"
	var framed, decoded bytes.Buffer
	if err := encodeLines(strings.NewReader(lines), &framed); err != nil {
		t.Fatal(err)
	}
	if framed.Len() != len(`{"id":1,"method":"hello"}`)+len(`{"id":2,"method":"session.list"}`)+8 {
		t.Errorf("framed length = %d, want two payloads plus two 4-byte headers", framed.Len())
	}
	if err := decodeFrames(&framed, &decoded); err != nil {
		t.Fatal(err)
	}
	want := `{"id":1,"method":"hello"}` + "\n" + `{"id":2,"method":"session.list"}` + "\n"
	if decoded.String() != want {
		t.Errorf("decoded = %q, want %q", decoded.String(), want)
	}
}

func TestDecodeRejectsTruncatedFrame(t *testing.T) {
	truncated := bytes.NewReader([]byte{0, 0, 0, 9, '{'})
	if err := decodeFrames(truncated, &bytes.Buffer{}); err == nil {
		t.Error("decodeFrames accepted a frame cut short")
	}
}
