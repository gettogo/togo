package sessiongen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Hook-driven agents (Copilot CLI, Droid, Cursor CLI, Amp) draw their chat
// from hooks: separate processes the agent starts, each given one JSON
// payload in the agent's own shape. A profile for such an agent writes
// the calls to Layout.Hooks and not records to a file. A test delivers
// them in order, or out of it, as the hook processes would; see
// e2e/hooks_driver_test.go.
//
// Only Copilot also keeps a log the adapter reads (the replies, which its
// hooks do not carry): a call lists the lines the agent has written to it
// by then in Writes, and the test appends them just before it delivers
// the call.

// HookCall is one hook the agent ran.
type HookCall struct {
	// Event and Payload are what `togo hook <agent> <Event>` receives:
	// the agent's own event name and payload.
	Event   string
	Payload map[string]any
	At      time.Time
	// Turn is the 1-based number of the prompt this call belongs to; 0
	// before the first prompt.
	Turn int
	// Session is the agent's own session id when the call was made.
	Session string
	// Writes are the lines the agent wrote to its own files just before.
	Writes []FileLine
	// End is how the turn ends when this is the call that ends it: "ok",
	// "interrupted" or "failed"; empty for every other call.
	End string
}

// FileLine is one JSON line appended to a file.
type FileLine struct {
	Path   string
	Record any
}

// JSON returns the payload as the agent writes it to a hook's stdin.
func (c HookCall) JSON() []byte {
	b, err := json.Marshal(c.Payload)
	if err != nil {
		panic(fmt.Sprintf("sessiongen: hook payload: %v", err))
	}
	return b
}

// Apply appends the call's file lines.
func (c HookCall) Apply() error {
	for _, w := range c.Writes {
		raw, err := json.Marshal(w.Record)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(w.Path), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(w.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		_, err = f.Write(append(raw, '\n'))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// HookTurn is what a hook-driven agent does in a turn besides the fields
// Turn has. A feature an agent has no hook for is an error, not ignored:
// the profile says what it cannot write.
type HookTurn struct {
	// Approve is how many of the turn's tool calls ask permission before
	// they run (Copilot's permissionRequest).
	Approve int
	// Fail is how many tool calls end in failure.
	Fail int
	// Ask is the agent's own question tool, asked after the tools.
	Ask *HookAsk
	// Resume starts the turn in an earlier conversation of the session,
	// as the agent's resume does: its start hook says so.
	Resume bool
	// Steer is a message typed while the turn runs: a second prompt
	// before the first one's stop.
	Steer bool
	// Pending leaves the last tool call, the question, or the approval
	// unanswered and the turn unfinished, as when the helper restarts
	// while the agent waits.
	Pending bool
	// Aborted ends the turn in the agent's own way of saying the user
	// stopped it (Interrupt does the same; Aborted names the agent's
	// second form where it has one).
	Aborted bool
	// Model is the model the payloads name, where they carry one.
	Model string
	// ErrorText is a provider error the turn ends with (Turn.Error ends
	// it failed; the text is what the agent said).
	ErrorText string
	// NoStop leaves out the hook that ends the turn, as when that hook
	// process failed.
	NoStop bool
	// Large is the size in bytes of one tool result, to reach a limit.
	Large int
}

// HookAsk is one question the agent asks.
type HookAsk struct {
	Prompt  string
	Options []string
	// Answer is the chosen option; empty with Cancel false leaves it
	// unanswered.
	Answer string
	Cancel bool
	// Plan is a plan the question asks to approve (Droid's spec).
	Plan string
}

// call records one hook call in the layout. at is taken from the clock.
func (g *Gen) call(session, event string, payload map[string]any, writes ...FileLine) HookCall {
	c := HookCall{Event: event, Payload: payload, At: g.Tick(), Turn: g.L.Truth.Prompts, Session: session, Writes: writes}
	g.L.Hooks = append(g.L.Hooks, c)
	return c
}

// ms is a time as the milliseconds since the epoch, Copilot's timestamp.
func ms(t time.Time) int64 { return t.UnixMilli() }

func uuid(g *Gen) string {
	return fmt.Sprintf("%s-%s-4%s-8%s-%s", g.Hex(8), g.Hex(4), g.Hex(3), g.Hex(3), g.Hex(12))
}

// ended marks the call just written as the end of its turn.
func (g *Gen) ended(state string) { g.L.Hooks[len(g.L.Hooks)-1].End = state }
