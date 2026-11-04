package rpc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// frameOf builds a complete wire frame (4-byte big-endian length header
// followed by the payload) for use as a fuzz seed.
func frameOf(payload string) []byte {
	out := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(out, uint32(len(payload)))
	copy(out[4:], payload)
	return out
}

func TestRoundTrip(t *testing.T) {
	small := []byte("{}")
	oneMiB := make([]byte, MaxFrame)
	for i := range oneMiB {
		oneMiB[i] = byte(i)
	}
	payloads := [][]byte{
		small,
		oneMiB,
	}
	for _, payload := range payloads {
		var buf bytes.Buffer
		if err := WriteFrame(&buf, payload); err != nil {
			t.Fatalf("WriteFrame(%d bytes): %v", len(payload), err)
		}
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("round trip: got %d bytes, want %d (equal content)", len(got), len(payload))
		}
	}
}

func TestReadFrameTooLarge(t *testing.T) {
	// Header 0x00100001 = MaxFrame + 1.
	r := bytes.NewReader([]byte{0x00, 0x10, 0x00, 0x01, 0xAA})
	_, err := ReadFrame(r)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("ReadFrame error = %v, want ErrFrameTooLarge", err)
	}
	if remaining := r.Len(); remaining != 1 {
		t.Fatalf("reader remaining = %d, want 1 (only 4 header bytes may be consumed)", remaining)
	}
}

func TestReadFrameHugeHeader(t *testing.T) {
	// 0xFFFFFFFF is a valid uint32 length but overflows int on 32-bit
	// platforms, where it must still be rejected as too large rather than
	// treated as a negative size.
	r := bytes.NewReader([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	_, err := ReadFrame(r)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("ReadFrame error = %v, want ErrFrameTooLarge", err)
	}
	if remaining := r.Len(); remaining != 0 {
		t.Fatalf("reader remaining = %d, want 0 (only 4 header bytes may be consumed)", remaining)
	}
}

var errBoom = errors.New("boom")

// headerThenErr returns the 4 header bytes of a frame declaring length 10,
// then fails every subsequent Read with errBoom.
type headerThenErr struct{ n int }

func (r *headerThenErr) Read(p []byte) (int, error) {
	if r.n < 4 {
		n := copy(p, []byte{0x00, 0x00, 0x00, 0x0A}[r.n:])
		r.n += n
		return n, nil
	}
	return 0, errBoom
}

func TestReadFramePropagatesReaderError(t *testing.T) {
	_, err := ReadFrame(&headerThenErr{})
	if !errors.Is(err, errBoom) {
		t.Fatalf("ReadFrame error = %v, want errBoom propagated", err)
	}
}

func TestReadFrameHeaderOnlyIsUnexpectedEOF(t *testing.T) {
	r := bytes.NewReader([]byte{0x00, 0x00, 0x00, 0x0A})
	_, err := ReadFrame(r)
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("ReadFrame error = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestReadFrameTruncated(t *testing.T) {
	data := make([]byte, 4+3)
	binary.BigEndian.PutUint32(data, 10)
	_, err := ReadFrame(bytes.NewReader(data))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadFrame error = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestReadFrameEOF(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader(nil))
	if err != io.EOF {
		t.Fatalf("ReadFrame error = %v, want exactly io.EOF", err)
	}
}

func TestWriteFrameTooLarge(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, make([]byte, MaxFrame+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("WriteFrame error = %v, want ErrFrameTooLarge", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("writer received %d bytes, want 0", buf.Len())
	}
}

func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{})
	f.Add(frameOf("hello"))
	f.Add(append(frameOf("hello"), 0x01, 0x02)) // trailing extra bytes
	f.Add([]byte{0x00, 0x00, 0x00, 0x05, 'a'})  // truncated body
	f.Add([]byte{0x00, 0x10, 0x00, 0x01, 0xAA}) // oversized header

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := ReadFrame(bytes.NewReader(data))
		if err == nil && len(got) > MaxFrame {
			t.Fatalf("ReadFrame returned %d-byte payload, exceeds MaxFrame %d", len(got), MaxFrame)
		}
	})
}
