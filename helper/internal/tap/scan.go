// Package tap finds notification requests in a pane's output stream and wires
// the scanner to tmux through pipe-pane.
package tap

import (
	"strings"
	"unicode"

	"github.com/gettogo/togo/helper/internal/wire"
)

// maxOSCBody bounds the bytes kept for one OSC sequence. A longer body is
// consumed to its terminator and dropped, so hostile output cannot grow the
// scanner.
const maxOSCBody = 1024

const (
	esc = 0x1b
	bel = 0x07
	can = 0x18
	sub = 0x1a
)

type state int

const (
	stGround state = iota
	stEsc
	stOSC
	stOSCEsc
	// stString covers DCS, SOS, PM and APC, which end only at ST. Their
	// content is never inspected, so a BEL inside is not a bell.
	stString
	stStringEsc
	// stCSI skips a control sequence's parameters to its final byte, and
	// stEscInter an escape's intermediate bytes, so neither reads as text.
	stCSI
	stEscInter
)

// Scanner extracts bell and notification signals from terminal output. State
// carries across Feed calls, so chunk boundaries may fall anywhere. The zero
// value is ready to use; a Scanner is not safe for concurrent use.
type Scanner struct {
	st       state
	body     []byte
	overflow bool
	// shell follows OSC 133 prompt marks into command blocks.
	shell shellBlock
}

// Feed consumes p and returns the signals completed by it. A BEL that
// terminates an OSC sequence is not a bell. Signals carry no session id.
func (s *Scanner) Feed(p []byte) []wire.Signal {
	var out []wire.Signal
	for _, b := range p {
		switch s.st {
		case stGround:
			switch b {
			case bel:
				out = append(out, wire.Signal{Kind: wire.SignalBell})
			case esc:
				s.st = stEsc
			default:
				s.shell.text(b)
			}
		case stEsc:
			switch {
			case b == '[':
				s.st = stCSI
				s.shell.csiStart()
			case b == ']':
				s.startOSC()
			case b == 'P' || b == 'X' || b == '^' || b == '_':
				s.st = stString
			case b == esc:
			case b >= 0x20 && b <= 0x2f:
				s.st = stEscInter
			default:
				s.st = stGround
			}
		case stCSI, stEscInter:
			switch {
			case b == esc:
				s.st = stEsc
			case b == can || b == sub:
				s.st = stGround
			case b == bel:
				// C0 controls act inside a control sequence.
				out = append(out, wire.Signal{Kind: wire.SignalBell})
			case s.st == stCSI && b >= 0x40 && b <= 0x7e:
				s.st = stGround
				s.shell.csi(b)
			case s.st == stCSI:
				s.shell.csiParam(b)
			case s.st == stEscInter && b >= 0x30 && b <= 0x7e:
				s.st = stGround
			}
		case stOSC:
			switch b {
			case bel:
				out = s.finishOSC(out)
			case esc:
				s.st = stOSCEsc
			case can, sub:
				s.reset()
			default:
				s.addBody(b)
			}
		case stOSCEsc:
			switch b {
			case '\\':
				out = s.finishOSC(out)
			case ']':
				s.startOSC()
			default:
				// ESC aborts the string and starts a new sequence.
				s.reset()
				if b == esc {
					s.st = stEsc
				}
			}
		case stString:
			switch b {
			case esc:
				s.st = stStringEsc
			case can, sub:
				s.reset()
			}
		case stStringEsc:
			switch b {
			case '\\':
				s.reset()
			case esc:
			default:
				s.st = stString
			}
		}
	}
	return out
}

func (s *Scanner) startOSC() {
	s.st = stOSC
	s.body = s.body[:0]
	s.overflow = false
}

func (s *Scanner) reset() {
	s.st = stGround
	s.body = s.body[:0]
	s.overflow = false
}

func (s *Scanner) addBody(b byte) {
	if s.overflow {
		return
	}
	if len(s.body) >= maxOSCBody {
		s.overflow = true
		// The head stays, so a prompt mark with long parameters still counts.
		s.body = s.body[:markHead]
		return
	}
	s.body = append(s.body, b)
}

func (s *Scanner) finishOSC(out []wire.Signal) []wire.Signal {
	body, overflow := string(s.body), s.overflow
	s.reset()
	if overflow {
		return s.shell.truncatedOSC(body, out)
	}
	if sig, ok := parseOSC(body); ok {
		out = append(out, sig)
	}
	return s.shell.osc(body, out)
}

// parseOSC turns an OSC body into a signal. OSC 9 is the iTerm2 notification;
// its "9;4;..." form reports progress and is not a notification. OSC 777
// carries rxvt-unicode's "notify;title;body".
func parseOSC(body string) (wire.Signal, bool) {
	switch {
	case strings.HasPrefix(body, "9;"):
		text := body[2:]
		if text == "4" || strings.HasPrefix(text, "4;") {
			return wire.Signal{}, false
		}
		text = clean(text)
		if text == "" {
			return wire.Signal{}, false
		}
		return wire.Signal{Kind: wire.SignalOSC9, Body: text}, true
	case strings.HasPrefix(body, "777;"):
		parts := strings.SplitN(body[4:], ";", 3)
		if parts[0] != "notify" || len(parts) < 2 {
			return wire.Signal{}, false
		}
		sig := wire.Signal{Kind: wire.SignalOSC777, Title: clean(parts[1])}
		if len(parts) == 3 {
			sig.Body = clean(parts[2])
		}
		if sig.Title == "" && sig.Body == "" {
			return wire.Signal{}, false
		}
		return sig, true
	}
	return wire.Signal{}, false
}

// clean makes remote-controlled text safe to forward: valid UTF-8 without
// control characters.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
