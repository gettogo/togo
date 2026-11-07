package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// ordered JSON: objects keep their key order so a scrubbed file differs
// from the original only by the removed entries.
type member struct {
	key string
	val any
}
type object []member

func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			var o object
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, ok := kt.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				v, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				o = append(o, member{k, v})
			}
			_, err := dec.Token()
			if o == nil {
				o = object{}
			}
			return o, err
		case '[':
			a := []any{}
			for dec.More() {
				v, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			_, err := dec.Token()
			return a, err
		}
		return nil, errors.New("unexpected delimiter")
	default:
		return t, nil
	}
}

// scrub drops members and elements naming marker; n counts them.
func scrub(v any, marker string, n *int) any {
	switch x := v.(type) {
	case object:
		out := object{}
		for _, m := range x {
			if strings.Contains(m.key, marker) {
				*n++
				continue
			}
			if s, ok := m.val.(string); ok && strings.Contains(s, marker) {
				*n++
				continue
			}
			out = append(out, member{m.key, scrub(m.val, marker, n)})
		}
		return out
	case []any:
		out := []any{}
		for _, e := range x {
			if s, ok := e.(string); ok && strings.Contains(s, marker) {
				*n++
				continue
			}
			out = append(out, scrub(e, marker, n))
		}
		return out
	}
	return v
}

func encodeOrdered(b *bytes.Buffer, v any, indent string, depth int) error {
	nl := func(d int) {
		if indent != "" {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(indent, d))
		}
	}
	sep := ":"
	if indent != "" {
		sep = ": "
	}
	switch x := v.(type) {
	case object:
		if len(x) == 0 {
			b.WriteString("{}")
			return nil
		}
		b.WriteByte('{')
		for i, m := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			nl(depth + 1)
			writeScalar(b, m.key)
			b.WriteString(sep)
			if err := encodeOrdered(b, m.val, indent, depth+1); err != nil {
				return err
			}
		}
		nl(depth)
		b.WriteByte('}')
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return nil
		}
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			nl(depth + 1)
			if err := encodeOrdered(b, e, indent, depth+1); err != nil {
				return err
			}
		}
		nl(depth)
		b.WriteByte(']')
	default:
		writeScalar(b, x)
	}
	return nil
}

func writeScalar(b *bytes.Buffer, v any) {
	if n, ok := v.(json.Number); ok {
		b.WriteString(n.String())
		return
	}
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	b.Write(bytes.TrimRight(tmp.Bytes(), "\n"))
}

// scrubJSON removes the entries naming marker from a JSON document and
// re-encodes it in its own indentation (two spaces, or compact).
func scrubJSON(raw []byte, marker string) ([]byte, int, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	if err != nil {
		return nil, 0, err
	}
	n := 0
	v = scrub(v, marker, &n)
	indent := ""
	if bytes.Contains(raw, []byte("\n  \"")) {
		indent = "  "
	} else if bytes.Contains(raw, []byte("\n\t\"")) {
		indent = "\t"
	}
	var b bytes.Buffer
	if err := encodeOrdered(&b, v, indent, 0); err != nil {
		return nil, 0, err
	}
	if bytes.HasSuffix(raw, []byte("\n")) {
		b.WriteByte('\n')
	}
	return b.Bytes(), n, nil
}
