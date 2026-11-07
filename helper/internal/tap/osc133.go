package tap

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

// SignalShell is the signal kind of a shell command block mark. Its Body is
// a Mark as JSON.
const SignalShell = "osc133"

// Mark kinds a shell signal carries.
const (
	// MarkStart: a command began running (OSC 133 C).
	MarkStart = "C"
	// MarkEnd: a command finished (OSC 133 D), or the next prompt arrived
	// without one.
	MarkEnd = "D"
)

// Limits for what a mark carries.
const (
	MaxCommand = 4 << 10 // command text, in bytes
	MaxOutput  = 4 << 10 // output tail, in bytes
	// markHead is how much of an oversized OSC body is kept, enough to read
	// a prompt mark's kind.
	markHead = 16
)

// Mark is one command block event from a shell with shell integration:
// FinalTerm / OSC 133 prompt marks (A prompt, B command line, C output, D
// finished with exit status), VS Code's OSC 633 twin with its E command
// line, and OSC 7 for the working directory.
type Mark struct {
	Mark    string `json:"mark"`
	Command string `json:"command,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	// Exit is the command's status, when the shell reported one.
	Exit *int `json:"exit,omitempty"`
	// Output is the tail of what the command printed, as plain text: escape
	// sequences dropped, redrawn lines settled. End marks only.
	Output string `json:"output,omitempty"`
}

type shellPhase int

const (
	shIdle shellPhase = iota
	shPrompt
	shInput
	shRunning
)

// shellBlock follows the marks of one pane. Between B and C it keeps the
// command line the shell echoed, as a small line editor (typing, backspace,
// cursor moves, erase to end of line), for shells that do not send the
// command explicitly. Between C and D it keeps the output tail.
type shellBlock struct {
	phase shellPhase
	// input is the echoed command line and at the editing cursor in it.
	input []rune
	at    int
	// entered is set once the line was entered: what the shell or the
	// command prints next is not the command line.
	entered bool
	// command is the command line the shell sent itself (633;E, or C with
	// cmdline or cmdline_url), preferred over the echo.
	command string
	cwd     string
	out     []byte
	// lineAt is where the current output line starts in out.
	lineAt int
	// cr is a carriage return not yet followed by anything.
	cr bool
	// csiParams holds the parameters of the control sequence being read.
	csiParams []byte
	// utf8 collects a multi-byte character of the echoed command line.
	utf8 []byte
}

// text takes one byte the terminal would print or act on as a C0 control.
func (b *shellBlock) text(c byte) {
	switch b.phase {
	case shInput:
		b.edit(c)
	case shRunning:
		b.output(c)
	}
}

func (b *shellBlock) edit(c byte) {
	if b.entered {
		return
	}
	switch {
	case c == '\n':
		b.entered = true
	case c == '\b' || c == 0x7f:
		b.utf8 = b.utf8[:0]
		if b.at > 0 {
			b.at--
		}
	case c == '\r':
		b.at = 0
	case c < 0x20:
	case c < 0x80:
		b.put(rune(c))
	default:
		b.utf8 = append(b.utf8, c)
		if utf8.FullRune(b.utf8) {
			r, _ := utf8.DecodeRune(b.utf8)
			b.utf8 = b.utf8[:0]
			b.put(r)
		} else if len(b.utf8) >= utf8.UTFMax {
			b.utf8 = b.utf8[:0]
		}
	}
}

// put writes r at the cursor, over what is there, as a terminal does.
func (b *shellBlock) put(r rune) {
	if b.at < len(b.input) {
		b.input[b.at] = r
	} else if len(b.input) < MaxCommand {
		b.input = append(b.input, r)
	} else {
		return
	}
	b.at++
}

func (b *shellBlock) output(c byte) {
	if b.cr {
		b.cr = false
		if c != '\n' {
			// A bare carriage return redraws the line, as a progress bar does.
			b.out = b.out[:b.lineAt]
		}
	}
	switch {
	case c == '\n':
		b.out = append(b.out, '\n')
		b.lineAt = len(b.out)
	case c == '\r':
		b.cr = true
	case c == '\b':
		if len(b.out) > b.lineAt {
			b.out = b.out[:len(b.out)-1]
		}
	case c == '\t' || c >= 0x20:
		b.out = append(b.out, c)
	}
	if len(b.out) > 2*MaxOutput {
		b.trimOutput()
	}
}

// trimOutput keeps the last MaxOutput bytes, starting at a whole character.
func (b *shellBlock) trimOutput() {
	cut := len(b.out) - MaxOutput
	for cut < len(b.out) && !utf8.RuneStart(b.out[cut]) {
		cut++
	}
	b.out = append(b.out[:0], b.out[cut:]...)
	b.lineAt = max(0, b.lineAt-cut)
}

func (b *shellBlock) csiStart() {
	b.csiParams = b.csiParams[:0]
}

func (b *shellBlock) csiParam(c byte) {
	if b.phase == shInput && len(b.csiParams) < 16 {
		b.csiParams = append(b.csiParams, c)
	}
}

// csi applies the cursor moves and erases a line editor uses to redraw the
// command line.
func (b *shellBlock) csi(final byte) {
	if b.phase != shInput || b.entered {
		return
	}
	n := 1
	if v, err := strconv.Atoi(string(b.csiParams)); err == nil && v > 0 {
		n = v
	}
	switch final {
	case 'D':
		b.at = max(0, b.at-n)
	case 'C':
		b.at = min(len(b.input), b.at+n)
	case 'G':
		b.at = 0
	case 'K':
		if len(b.csiParams) == 0 || string(b.csiParams) == "0" {
			b.input = b.input[:min(b.at, len(b.input))]
		} else if string(b.csiParams) == "2" {
			b.input, b.at = b.input[:0], 0
		}
	}
}

// truncatedOSC reads the mark kind of an OSC body too long to keep.
func (b *shellBlock) truncatedOSC(head string, out []wire.Signal) []wire.Signal {
	kind, _, ok := markOf(head)
	if !ok {
		return out
	}
	return b.mark(kind, "", out)
}

// osc handles a complete OSC body.
func (b *shellBlock) osc(body string, out []wire.Signal) []wire.Signal {
	if strings.HasPrefix(body, "7;") {
		if cwd := cwdFromURL(body[2:]); cwd != "" {
			b.cwd = cwd
		}
		return out
	}
	kind, params, ok := markOf(body)
	if !ok {
		return out
	}
	return b.mark(kind, params, out)
}

// markOf splits "133;C;k=v" or "633;E;text" into its kind and parameters.
func markOf(body string) (kind, params string, ok bool) {
	rest, found := strings.CutPrefix(body, "133;")
	if !found {
		if rest, found = strings.CutPrefix(body, "633;"); !found {
			return "", "", false
		}
	}
	if rest == "" {
		return "", "", false
	}
	kind, params, _ = strings.Cut(rest, ";")
	return kind, params, len(kind) == 1
}

func (b *shellBlock) mark(kind, params string, out []wire.Signal) []wire.Signal {
	switch kind {
	case "A":
		if b.phase == shRunning {
			// The next prompt came without an end mark: the command is over,
			// its status unknown.
			out = b.finish(nil, out)
		}
		b.phase = shPrompt
		b.command = ""
	case "B":
		b.phase = shInput
		b.input, b.at, b.entered = b.input[:0], 0, false
	case "E":
		b.command = cleanCommand(unescape633(params))
	case "P":
		if v, found := strings.CutPrefix(params, "Cwd="); found {
			b.cwd = cleanLine(unescape633(v))
		}
	case "C":
		if cmd := commandParam(params); cmd != "" {
			b.command = cmd
		}
		cmd := b.commandLine()
		b.phase = shRunning
		b.out, b.lineAt, b.cr = b.out[:0], 0, false
		out = append(out, b.signal(Mark{Mark: MarkStart, Command: cmd, Cwd: b.cwd}))
	case "D":
		var exit *int
		code, _, _ := strings.Cut(params, ";")
		if v, err := strconv.Atoi(code); err == nil {
			exit = &v
		}
		switch b.phase {
		case shRunning:
			out = b.finish(exit, out)
		case shInput:
			// A shell that marks no output start: the block is the command
			// line alone.
			if cmd := b.commandLine(); cmd != "" {
				out = append(out, b.signal(Mark{Mark: MarkEnd, Command: cmd, Cwd: b.cwd, Exit: exit}))
			}
		}
		b.phase = shIdle
	}
	return out
}

func (b *shellBlock) finish(exit *int, out []wire.Signal) []wire.Signal {
	if len(b.out) > MaxOutput {
		b.trimOutput()
	}
	m := Mark{Mark: MarkEnd, Command: b.commandLine(), Cwd: b.cwd, Exit: exit, Output: cleanOutput(b.out)}
	b.phase = shIdle
	b.out, b.lineAt, b.cr = b.out[:0], 0, false
	return append(out, b.signal(m))
}

// commandLine is the explicit command when the shell sent one, else the
// echoed line.
func (b *shellBlock) commandLine() string {
	if b.command != "" {
		return b.command
	}
	return cleanCommand(string(b.input))
}

func (b *shellBlock) signal(m Mark) wire.Signal {
	body, _ := json.Marshal(m)
	return wire.Signal{Kind: SignalShell, Title: m.Mark, Body: string(body), Code: m.Exit}
}

// commandParam reads kitty's and fish's cmdline or cmdline_url parameter of
// a C mark.
func commandParam(params string) string {
	for _, p := range strings.Split(params, ";") {
		if v, ok := strings.CutPrefix(p, "cmdline_url="); ok {
			if d, err := url.PathUnescape(v); err == nil {
				return cleanCommand(d)
			}
		}
		if v, ok := strings.CutPrefix(p, "cmdline="); ok {
			return cleanCommand(v)
		}
	}
	return ""
}

// unescape633 undoes VS Code's escaping: \\ for a backslash and \xAB for
// any byte.
func unescape633(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			out = append(out, s[i])
			continue
		}
		switch {
		case s[i+1] == '\\':
			out = append(out, '\\')
			i++
		case s[i+1] == 'x' && i+3 < len(s):
			if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				out = append(out, byte(v))
				i += 3
				continue
			}
			out = append(out, s[i])
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// cwdFromURL reads OSC 7's file://host/path.
func cwdFromURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return cleanLine(u.Path)
}

func cleanCommand(s string) string {
	s = strings.TrimSpace(cleanLine(s))
	if len(s) > MaxCommand {
		s = strings.ToValidUTF8(s[:MaxCommand], "")
	}
	return s
}

// cleanLine is valid UTF-8 without control characters.
func cleanLine(s string) string {
	return clean(s)
}

// cleanOutput keeps newlines and tabs and drops other controls.
func cleanOutput(p []byte) string {
	s := strings.ToValidUTF8(string(p), "�")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimRight(s, "\n ")
}
