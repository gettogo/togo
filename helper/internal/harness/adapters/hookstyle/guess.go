package hookstyle

import (
	"context"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/source"
)

// guessEvery is how often Options.Transcript is asked again.
const guessEvery = 5 * time.Second

// guessWait bounds one Options.Transcript call: a folder walk, a lookup
// of open files and one of processes.
const guessWait = 8 * time.Second

// guess is the transcript Options.Transcript found, looked for in a
// goroutine of its own: the walk and the process lookups it makes never
// run under the session's lock (rule: no blocking call under it).
type guess struct {
	at      time.Time // when the last look began
	running bool
	found   string // the last answer, applied by State
	wake    func()
	solo    source.Solo
}

// Wake keeps the engine's wake, called when a look ends (harness.Waker).
func (m *Mapper) Wake(f func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guess.wake = f
}

// guessLocked applies the last answer and, at most every guessEvery,
// starts another look. The caller holds m.mu.
func (m *Mapper) guessLocked() {
	if m.hooked || m.opts.Transcript == nil {
		return
	}
	g := &m.guess
	if g.found != "" {
		m.state["transcript_path"] = g.found
	}
	if g.running || time.Since(g.at) < guessEvery {
		return
	}
	g.running, g.at = true, time.Now()
	env, current, wake := m.env, m.state["transcript_path"], g.wake
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), guessWait)
		p := m.opts.Transcript(ctx, env, current, func() bool { return g.solo.Check(env) })
		cancel()
		m.mu.Lock()
		g.running = false
		changed := p != "" && p != g.found
		if p != "" {
			g.found = p
		}
		m.mu.Unlock()
		if changed && wake != nil {
			wake()
		}
	}()
}
