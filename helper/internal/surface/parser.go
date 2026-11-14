// Package surface carries bounded Tern Surface Protocol documents and input
// between a managed pane and its native renderer.
package surface

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

// Protocol and transport limits. Large documents travel in bounded RPC chunks.
const (
	MaxAPCBytes     = 65536
	MaxStringBytes  = 262144
	MaxMessageBytes = 8 << 20
	ChunkTimeout    = time.Second
)

var (
	// ErrLimit means a string or assembled message exceeded its byte budget.
	ErrLimit = errors.New("surface: message exceeds its byte limit")
	// ErrMalformed means the TSP envelope is invalid, without exposing its body.
	ErrMalformed = errors.New("surface: malformed message envelope")
)

// Message is one complete TSP message. Body may be JSON or blob base64.
type Message struct {
	Verb   byte
	Params map[string]string
	Body   json.RawMessage
}

const (
	ground = iota
	escape
	ignored
	ignoredEscape
	application
	applicationEscape
)

// Parser extracts APC messages from raw pane bytes. The zero value is ready;
// calls for a pane must be serialized. Feed does not acknowledge frames.
type Parser struct {
	state    int
	body     []byte
	overflow bool
	chunks   chunkSequence
}

// Reset discards fragments and chunks when the originating program changes.
func (p *Parser) Reset() {
	*p = Parser{}
}

// Feed accepts arbitrary byte boundaries. An empty call expires stalled chunks.
func (p *Parser) Feed(data []byte, now time.Time) (messages []Message, errs []error) {
	if err := p.chunks.expire(now); err != nil {
		errs = append(errs, err)
	}
	finish := func() {
		if bytes.HasPrefix(p.body, []byte("tsp;")) {
			if p.overflow {
				errs = append(errs, ErrLimit)
			} else if m, err := parseMessage(p.body[4:]); err != nil {
				errs = append(errs, err)
			} else {
				joined, err := p.chunks.push(m, now)
				if err != nil {
					errs = append(errs, err)
				}
				if joined != nil {
					messages = append(messages, *joined)
				}
			}
		}
		p.body = p.body[:0]
		p.overflow = false
		p.state = ground
	}
	for _, b := range data {
		switch p.state {
		case ground:
			if b == 0x1b {
				p.state = escape
			}
		case escape:
			switch b {
			case '_':
				p.body = p.body[:0]
				p.overflow = false
				p.state = application
			case ']', 'P', 'X', '^':
				p.state = ignored
			case 'c': // RIS also drops an unfinished chunk sequence.
				p.Reset()
			case 0x1b:
			default:
				p.state = ground
			}
		case ignored:
			switch b {
			case 0x1b:
				p.state = ignoredEscape
			case 0x07, 0x18, 0x1a:
				p.state = ground
			}
		case ignoredEscape:
			if b == '\\' || b == 0x18 || b == 0x1a {
				p.state = ground
			} else if b != 0x1b {
				p.state = ignored
			}
		case application:
			switch b {
			case 0x1b:
				p.state = applicationEscape
			case 0x07, 0x18, 0x1a:
				p.body = p.body[:0]
				p.state = ground
			default:
				if len(p.body) >= MaxStringBytes {
					p.overflow = true
				} else {
					p.body = append(p.body, b)
				}
			}
		case applicationEscape:
			if b == '\\' {
				finish()
			} else {
				// Raw ESC cannot occur in a JSON body. Abandon this string;
				// do not reinterpret its tail as another TSP envelope.
				p.body = p.body[:0]
				p.state = ignored
			}
		}
	}
	return messages, errs
}

func parseMessage(raw []byte) (Message, error) {
	if len(raw) < 2 || raw[1] != ';' || !letter(raw[0]) {
		return Message{}, ErrMalformed
	}
	m := Message{Verb: raw[0], Params: map[string]string{}}
	body := raw[2:]
	for {
		part, rest, ok := bytes.Cut(body, []byte(";"))
		key, value, param := bytes.Cut(part, []byte("="))
		if !ok || !param || !parameter(key, value) {
			break
		}
		if len(m.Params) >= 32 || len(key) > 64 || len(value) > 256 {
			return Message{}, ErrMalformed
		}
		m.Params[string(key)] = string(value)
		body = rest
	}
	if len(body) > MaxAPCBytes {
		return Message{}, ErrLimit
	}
	m.Body = bytes.Clone(body)
	return m, nil
}

func letter(b byte) bool { return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' }

func parameter(key, value []byte) bool {
	if len(key) == 0 {
		return false
	}
	for _, b := range key {
		if !letter(b) && !(b >= '0' && b <= '9') && b != '_' && b != '-' {
			return false
		}
	}
	for _, b := range value {
		if b <= 0x20 || b >= 0x7f || b == ';' {
			return false
		}
	}
	return true
}

// Encode builds one terminal-to-program message, which TSP never chunks.
func Encode(verb byte, body any) ([]byte, error) {
	if verb != 'e' && verb != 'r' {
		return nil, ErrMalformed
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxAPCBytes {
		return nil, ErrLimit
	}
	data := append([]byte{'\x1b', '_', 't', 's', 'p', ';', verb, ';'}, raw...)
	return append(data, '\x1b', '\\'), nil
}
