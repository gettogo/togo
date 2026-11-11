package watch

import "time"

// sessionState is what the watcher knows about one managed session.
type sessionState struct {
	// attached is true once the output tap is piped to the pane.
	attached bool
	// goneScans counts consecutive start scans that found the agent gone.
	goneScans int
	// working is true after output until the session settles.
	working bool
	// awaitingIdle is set by output and cleared when the silence check runs,
	// so a session that never produced output never gets an idle verdict.
	awaitingIdle bool
	lastOutput   time.Time
	// screenCheck asks the next tick to look for a prompt that was drawn
	// before the tap was attached, such as one left on screen across a
	// daemon restart. Nothing else reveals it while the agent waits.
	screenCheck bool
	// prompt is the fingerprint of the prompt reported to the PromptSink,
	// empty when none is pending.
	prompt string
	// promptSnippet is the screen around that prompt when it was reported,
	// to tell a re-asked prompt from the same one still up.
	promptSnippet string
	// lastInspect is when an attention signal last captured the screen;
	// lastNotify, notifyTitle and notifyBody describe the last needs_you
	// notification. Together they bound what a program ringing the bell in
	// a loop can cost.
	lastInspect, lastNotify time.Time
	// lastPoll is when a coding agent's screen was last read for a prompt
	// while its output went on.
	lastPoll                time.Time
	notifyTitle, notifyBody string

	// agent is the session's agent id from the listing; it picks how
	// output is read (see busy.go).
	agent string
	// attachedAt is when the tap was attached; output soon after is the
	// program starting up, not work.
	attachedAt time.Time
	// geom is the pane size and client count last seen with output, and
	// quietUntil ends the redraw window a change of it opens.
	geom       string
	quietUntil time.Time
	// textSum is the screen's text when an agent without a working
	// indicator last wrote to it, read at textAt; textSame is whether that
	// read found it unchanged (see textUnchanged).
	textSum  [32]byte
	textAt   time.Time
	textSame bool
	// burstStart is when the current run of output began.
	burstStart time.Time
	// promptCheck asks for a prompt check once output that did not mark
	// the session working falls silent.
	promptCheck bool
	// lastBusyCheck is when the screen was last read for the agent's
	// working indicator; busyMisses counts reads in a row without it.
	lastBusyCheck time.Time
	busyMisses    int
	// vouchedSince is when a harness feed's word first kept this silent
	// session working, since the pane last showed the agent busy;
	// busyHold is when the indicator is read again after such a vouch
	// (see settle).
	vouchedSince time.Time
	busyHold     time.Time
	// lastSignInCheck is when the screen of a program whose output never
	// stops was last read for a sign-in step (see signin.go).
	lastSignInCheck time.Time
}

// outputSeen records an output burst and reports whether the session just
// became busy.
func (s *sessionState) outputSeen(now time.Time) (becameWorking bool) {
	s.lastOutput = now
	s.awaitingIdle = true
	becameWorking = !s.working
	s.working = true
	return becameWorking
}

// silent reports whether the idle window has elapsed since the last output.
func (s *sessionState) silent(now time.Time, idle time.Duration) bool {
	return s.awaitingIdle && now.Sub(s.lastOutput) >= idle
}
