package source

import (
	"bytes"
	"encoding/json"
	"os"
)

// A replay cut to a file's last records keeps the file's header: the
// record that names the session, which the mapper needs to know whose
// file it reads. Most files open with it. One that opens with other
// records first (omp writes a title record, then its session record)
// names the header's type in the spec, and the records up to and
// including it are kept.

// firstHeaderMax bounds how much of a file's start is read for its header.
const firstHeaderMax = 64 << 10

// headMax bounds the leading records searched for the header.
const headMax = 8

// headRecords reads a file's header from its start: its leading records
// (see leading), each a whole line that is a JSON value; nil when the
// first line is not one.
func headRecords(fh *os.File, typ string) []json.RawMessage {
	buf := make([]byte, firstHeaderMax)
	n, _ := fh.ReadAt(buf, 0)
	rest := buf[:n]
	var out []json.RawMessage
	for len(out) < headMax {
		i := bytes.IndexByte(rest, '\n')
		if i <= 0 {
			break
		}
		rs := Records(rest[:i], 1)
		if len(rs) != 1 {
			break
		}
		out = append(out, rs[0])
		rest = rest[i+1:]
		if typ == "" {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return leading(out, typ)
}

// leading is the header among a file's first records: those up to and
// including the first whose "type" is typ, within headMax; else, or with
// no typ, the first record alone.
func leading(recs []json.RawMessage, typ string) []json.RawMessage {
	if len(recs) == 0 {
		return nil
	}
	if typ != "" {
		for i := 0; i < len(recs) && i < headMax; i++ {
			var r struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(recs[i], &r) == nil && r.Type == typ {
				return recs[:i+1]
			}
		}
	}
	return recs[:1]
}
