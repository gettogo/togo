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
	// lastInspect is when an attention signal last captured the screen;
	// lastNotify, notifyTitle and notifyBody describe the last needs_you
	// notification. Together they bound what a program ringing the bell in
	// a loop can cost.
	lastInspect, lastNotify time.Time
	notifyTitle, notifyBody string
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
