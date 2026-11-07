package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// object is a JSON object that keeps its keys in the file's order, so a
// settings write changes one value and leaves the rest of the user's file
// as it was laid out (a map would sort every key).
type object struct {
	keys []string
	vals map[string]any
}

func newObject() *object { return &object{vals: map[string]any{}} }

func (o *object) get(k string) (any, bool) {
	v, ok := o.vals[k]
	return v, ok
}

func (o *object) set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// decodeOrdered reads one JSON value: objects as *object, numbers as
// json.Number (written back as they were), the rest as encoding/json does.
func decodeOrdered(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	v, err := readValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("json: data after the value")
	}
	return v, nil
}

func readValue(dec *json.Decoder) (any, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch d := t.(type) {
	case json.Delim:
		switch d {
		case '{':
			o := newObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := readValue(dec)
				if err != nil {
					return nil, err
				}
				o.set(k, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return o, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := readValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, errors.New("json: unexpected delimiter")
	}
	return t, nil
}

// encodeOrdered writes v indented by two spaces, keys in their order.
func encodeOrdered(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeOrdered(&b, v, 0); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeOrdered(b *bytes.Buffer, v any, depth int) error {
	pad := func(d int) { b.WriteString("\n" + strings.Repeat("  ", d)) }
	switch x := v.(type) {
	case *object:
		if len(x.keys) == 0 {
			b.WriteString("{}")
			return nil
		}
		b.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			pad(depth + 1)
			if err := writeScalar(b, k); err != nil {
				return err
			}
			b.WriteString(": ")
			if err := writeOrdered(b, x.vals[k], depth+1); err != nil {
				return err
			}
		}
		pad(depth)
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
			pad(depth + 1)
			if err := writeOrdered(b, e, depth+1); err != nil {
				return err
			}
		}
		pad(depth)
		b.WriteByte(']')
	default:
		return writeScalar(b, x)
	}
	return nil
}

func writeScalar(b *bytes.Buffer, v any) error {
	var s bytes.Buffer
	enc := json.NewEncoder(&s)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	b.Write(bytes.TrimRight(s.Bytes(), "\n"))
	return nil
}
