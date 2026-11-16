package copilot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/source"
)

// Copilot's hooks carry no reply text. What it says is in its own session
// log, ~/.copilot/session-state/<session id>/events.jsonl, as
// assistant.message records (Copilot CLI 1.0.107, checked live): content is
// the text, empty when the message is only tool requests.

// sessionID is the shape of Copilot's session ids; anything else is not
// looked up, so a payload cannot name a path outside session-state.
var sessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{7,63}$`)

// maxRead bounds one read of the log; the next read goes on from there.
const maxRead = 4 << 20

func eventsPath(env harness.Env, session string) string {
	home := source.Expand("~", env.Vars[source.VarHome])
	return filepath.Join(home, ".copilot", "session-state", session, "events.jsonl")
}

// replies reads the agent messages written to session's log after
// offset. It suits hookstyle.Options.Replies.
func replies(env harness.Env, session string, offset int64) ([]hookstyle.Reply, int64) {
	if !sessionID.MatchString(session) {
		return nil, 0
	}
	f, err := os.Open(eventsPath(env, session))
	if err != nil {
		return nil, max(offset, 0)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, max(offset, 0)
	}
	if offset < 0 || offset > st.Size() {
		// From the end, or the log was replaced: only what comes next.
		return nil, st.Size()
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset
	}
	var out []hookstyle.Reply
	start := offset
	r := bufio.NewReader(io.LimitReader(f, maxRead))
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			if offset+int64(len(line))-start >= maxRead {
				// The read limit cut a line that is longer than a read
				// (a large tool result): step over it, else every later
				// read stops at the same place.
				return out, skipLine(f, offset+int64(len(line)), offset)
			}
			// A line without its newline is still being written.
			return out, offset
		}
		offset += int64(len(line))
		if !bytes.Contains(line, []byte(`"assistant.message"`)) {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Data struct {
				Content          string `json:"content"`
				ParentToolCallID string `json:"parentToolCallId"`
				ToolRequests     []struct {
					ToolCallID string `json:"toolCallId"`
				} `json:"toolRequests"`
			} `json:"data"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type != "assistant.message" || ev.Data.ParentToolCallID != "" {
			continue
		}
		r := hookstyle.Reply{Text: ev.Data.Content}
		for _, c := range ev.Data.ToolRequests {
			r.Calls = append(r.Calls, c.ToolCallID)
		}
		out = append(out, r)
	}
}

// skipLine returns the offset just past the line that runs through from,
// or fallback when its newline is not written yet.
func skipLine(f *os.File, from, fallback int64) int64 {
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return fallback
	}
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		chunk, err := r.ReadSlice('\n')
		from += int64(len(chunk))
		switch {
		case err == nil:
			return from
		case err == bufio.ErrBufferFull:
		default:
			return fallback
		}
	}
}
