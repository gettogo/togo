package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gettogo/togo/helper/internal/rpc"
)

const (
	// DefaultCaptureLines is the history depth used when a request omits lines.
	DefaultCaptureLines = 1000
	maxCaptureLines     = 10000
	// maxCaptureBytes caps the raw text. The frame limit is checked
	// against the encoded reply, which keepFitting enforces separately.
	maxCaptureBytes = 900000
	// envelopeHeadroom is left in the frame for the JSON-RPC envelope and
	// the truncated field around the encoded text.
	envelopeHeadroom = 1024
)

// Capture returns the plain text of a session's scrollback (lines rows above
// the screen) and visible rows. It reads managed and unmanaged sessions
// alike and changes nothing. Trailing blank rows of the screen are dropped.
// Text too large for one protocol frame keeps its newest lines and reports
// truncated.
func (s *Service) Capture(ctx context.Context, id string, lines int, join bool) (text string, truncated bool, err error) {
	if lines < 1 || lines > maxCaptureLines {
		return "", false, &InvalidParamsError{Msg: fmt.Sprintf("lines must be between 1 and %d", maxCaptureLines)}
	}
	sess, err := s.find(ctx, id)
	if err != nil {
		return "", false, err
	}
	client, name := s.Tmux, sess.ID
	if !sess.Managed {
		client, name = s.User, sess.Name
	}
	raw, err := client.CaptureHistory(ctx, name, lines, join)
	if err != nil {
		return "", false, notFoundOr(err, id)
	}
	text, truncated = keepNewest(strings.TrimRight(raw, "\n"), maxCaptureBytes)
	text, dropped := keepFitting(text, rpc.MaxFrame-envelopeHeadroom)
	return text, truncated || dropped, nil
}

// keepNewest returns the tail of text that fits in limit bytes, starting at
// a line boundary so a line is never cut in half or through a UTF-8 rune.
func keepNewest(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	cut := len(text) - limit
	if text[cut-1] == '\n' {
		return text[cut:], true
	}
	// The cut lands inside a line, so drop that partial line.
	nl := strings.IndexByte(text[cut:], '\n')
	if nl < 0 {
		return "", true
	}
	return text[cut+nl+1:], true
}

// keepFitting returns the newest whole lines of text whose JSON encoding
// fits in budget bytes. The server encodes replies with json.Marshal, which
// turns "<", ">" and "&" into 6 bytes and quotes, backslashes and newlines
// into 2, so the raw length says little about the frame size.
func keepFitting(text string, budget int) (string, bool) {
	const quotes = 2 // the surrounding " of the encoded string
	used := quotes
	kept := len(text) // text[kept:] is what fits so far
	end := len(text)  // end of the line under consideration
	for end >= 0 {
		lineStart := strings.LastIndexByte(text[:end], '\n') + 1
		cost := encodedLen(text[lineStart:end])
		if end < len(text) {
			cost += 2 // the "\n" that follows this line
		}
		if used+cost > budget {
			return text[kept:], true
		}
		used += cost
		kept = lineStart
		end = lineStart - 1
	}
	return text, false
}

func encodedLen(s string) int {
	b, _ := json.Marshal(s) // marshalling a string cannot fail
	return len(b) - 2
}
