// Package rpc implements framing for the Togo helper protocol:
// 4-byte big-endian unsigned length headers followed by JSON-RPC 2.0 payloads.
package rpc

import (
	"encoding/binary"
	"errors"
	"io"
)

// MaxFrame is the maximum payload size, in bytes, of a single frame (1 MiB).
const MaxFrame = 1 << 20

// ErrFrameTooLarge is returned when a frame payload would exceed MaxFrame.
var ErrFrameTooLarge = errors.New("rpc: frame too large")

// ReadFrame reads one frame from r: a 4-byte big-endian unsigned length
// header followed by that many payload bytes.
//
// Errors:
//   - io.EOF if r holds no header bytes at all (clean end of stream).
//   - ErrFrameTooLarge if the declared length exceeds MaxFrame; in that
//     case only the 4 header bytes have been consumed from r.
//   - io.ErrUnexpectedEOF if the stream ends before the full payload
//     (including a header-only stream with zero body bytes read).
//   - Any other error from the underlying reader, propagated unchanged
//     after a full header has been read.
func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n > MaxFrame {
		return nil, ErrFrameTooLarge
	}
	size := int(n)
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return payload, nil
}

// WriteFrame writes one frame to w: a 4-byte big-endian unsigned length
// header followed by payload, issued as a single Write call.
//
// Payloads larger than MaxFrame are rejected with ErrFrameTooLarge before
// anything is written to w.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxFrame {
		return ErrFrameTooLarge
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[4:], payload)
	_, err := w.Write(frame)
	return err
}
