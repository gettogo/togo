package hookstyle

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// ToolTitle names a tool call from its name and input: the shell command,
// the file it touches, or the tool's own name. Harnesses name the same
// tools differently (Bash, run_shell_command, bash, shell), so the input's
// fields decide more than the name.
func ToolTitle(name string, input json.RawMessage) (string, string, *wire.Subject) {
	var in struct {
		Command  json.RawMessage `json:"command"`
		FilePath string          `json:"file_path"`
		Path     string          `json:"path"`
		Absolute string          `json:"absolute_path"`
		Pattern  string          `json:"pattern"`
		URL      string          `json:"url"`
		Query    string          `json:"query"`
	}
	_ = json.Unmarshal(input, &in)
	var cmd string
	if json.Unmarshal(in.Command, &cmd) != nil {
		var argv []string
		if json.Unmarshal(in.Command, &argv) == nil {
			cmd = strings.Join(argv, " ")
		}
	}
	low := strings.ToLower(name)
	file := firstOf(in.FilePath, in.Absolute, in.Path)
	switch {
	case cmd != "":
		return cmd, harness.ToolShell, &wire.Subject{Kind: wire.SubjectCommand, Text: Truncate(cmd, wire.MaxSubjectText)}
	case strings.HasPrefix(name, "mcp__"):
		return strings.TrimPrefix(name, "mcp__"), harness.ToolMCP, nil
	case in.URL != "" || strings.Contains(low, "web") || strings.Contains(low, "fetch"):
		t := firstOf(in.URL, in.Query)
		if t == "" {
			return name, harness.ToolWeb, nil
		}
		return t, harness.ToolWeb, &wire.Subject{Kind: wire.SubjectURL, Text: Truncate(t, wire.MaxSubjectText)}
	case strings.Contains(low, "todo"):
		return name, harness.ToolOther, nil
	case strings.Contains(low, "edit") || strings.Contains(low, "patch") || strings.Contains(low, "replace"):
		return titled("Edit", file, name), harness.ToolEdit, fileSubject(file)
	case strings.Contains(low, "write") || strings.Contains(low, "create"):
		return titled("Write", file, name), harness.ToolWrite, fileSubject(file)
	case strings.Contains(low, "grep") || strings.Contains(low, "glob") || strings.Contains(low, "search") || in.Pattern != "":
		return titled("Search", firstOf(in.Pattern, file), name), harness.ToolSearch, nil
	case strings.Contains(low, "read") || strings.Contains(low, "view") || strings.Contains(low, "ls"):
		return titled("Read", file, name), harness.ToolRead, fileSubject(file)
	case low == "task" || low == "agent" || strings.Contains(low, "subagent"):
		return name, harness.ToolAgent, nil
	case file != "":
		return name + " " + file, harness.ToolOther, fileSubject(file)
	}
	return name, harness.ToolOther, nil
}

func titled(verb, what, name string) string {
	if what == "" {
		return name
	}
	return verb + " " + what
}

func fileSubject(p string) *wire.Subject {
	if p == "" {
		return nil
	}
	return &wire.Subject{Kind: wire.SubjectFile, Text: Truncate(p, wire.MaxSubjectText)}
}

// ToolOutput reads a tool response: a string, or an object's output,
// stdout, content or textResultForLlm field, or else its compact JSON.
// failed is true when the response says the call failed.
func ToolOutput(raw json.RawMessage) (out string, failed bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return Tail(s, harness.MaxToolOutput), false
	}
	var o map[string]any
	if json.Unmarshal(raw, &o) == nil {
		for _, k := range []string{"resultType", "status"} {
			if v, _ := o[k].(string); v == "failure" || v == "error" || v == "failed" || v == "denied" || v == "rejected" {
				failed = true
			}
		}
		if v, ok := o["success"].(bool); ok && !v {
			failed = true
		}
		for _, k := range []string{"output", "stdout", "content", "textResultForLlm", "result", "error"} {
			if v, ok := o[k].(string); ok && v != "" {
				return Tail(v, harness.MaxToolOutput), failed
			}
		}
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return "", failed
	}
	return Tail(b.String(), harness.MaxToolOutput), failed
}

// Truncate keeps at most n runes, ending in an ellipsis when cut.
func Truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// Tail keeps the last n bytes of s on a rune boundary.
func Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// Clip bounds item text to harness.MaxItemText bytes.
func Clip(s string) string {
	if len(s) <= harness.MaxItemText {
		return s
	}
	s = s[:harness.MaxItemText]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func firstOf(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// live records the sessions whose chat a transcript feed already draws, so
// the hook mapper of the same session keeps state without drawing every
// turn twice. Bounded: the oldest entry goes first.
var live = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

const maxLive = 512

// MarkLive says a transcript feed draws the session's chat.
func MarkLive(session string) {
	if session == "" {
		return
	}
	live.Lock()
	defer live.Unlock()
	if _, ok := live.at[session]; !ok && len(live.at) >= maxLive {
		oldest, when := "", time.Time{}
		for k, v := range live.at {
			if oldest == "" || v.Before(when) {
				oldest, when = k, v
			}
		}
		delete(live.at, oldest)
	}
	live.at[session] = time.Now()
}

// UnmarkLive says the session's transcript feed stopped.
func UnmarkLive(session string) {
	live.Lock()
	defer live.Unlock()
	delete(live.at, session)
}

// IsLive reports whether a transcript feed draws the session's chat.
func IsLive(session string) bool {
	live.Lock()
	defer live.Unlock()
	_, ok := live.at[session]
	return ok
}

// drawn records, per session, the turn the hook mapper drew while no
// transcript feed drew the chat. A session's transcript file appears only
// with its first prompt, so that prompt's hook usually comes first and is
// drawn; the transcript mapper then claims the turn (ClaimTurn) and goes
// on under the same id instead of drawing the prompt a second time.
var drawn = struct {
	sync.Mutex
	turn map[string]drawnTurn
}{turn: map[string]drawnTurn{}}

type drawnTurn struct {
	id      string
	started time.Time
}

func noteDrawn(session, id string, started time.Time) {
	if session == "" || id == "" {
		return
	}
	drawn.Lock()
	defer drawn.Unlock()
	if _, ok := drawn.turn[session]; !ok && len(drawn.turn) >= maxLive {
		for k := range drawn.turn {
			delete(drawn.turn, k)
			break
		}
	}
	drawn.turn[session] = drawnTurn{id: id, started: started}
}

func clearDrawn(session, id string) {
	drawn.Lock()
	defer drawn.Unlock()
	if d, ok := drawn.turn[session]; ok && d.id == id {
		delete(drawn.turn, session)
	}
}

// claimWindow is how far apart a hook's prompt and the transcript's record
// of it may be and still be the same prompt.
const claimWindow = 30 * time.Second

// ClaimTurn hands a transcript mapper the open turn the hook mapper drew
// for the prompt the transcript now records at at. The hook mapper drew
// that prompt already, so the transcript draws only what follows, under
// the returned turn id.
func ClaimTurn(session string, at time.Time) (id string, started time.Time, ok bool) {
	drawn.Lock()
	defer drawn.Unlock()
	d, found := drawn.turn[session]
	if !found {
		return "", time.Time{}, false
	}
	delete(drawn.turn, session)
	if gap := at.Sub(d.started); gap > claimWindow || gap < -claimWindow {
		return "", time.Time{}, false
	}
	return d.id, d.started, true
}
