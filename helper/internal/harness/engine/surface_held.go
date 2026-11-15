package engine

import "github.com/gettogo/togo/helper/internal/harness"

// While a phone holds a session's terminal natively, the agent draws its
// screens for the phone and takes its input from there. Keys put into the
// pane then look like someone typing at the desk, which hands the terminal
// back and ends the native screen in the middle of a picker or a form. So
// nothing is typed into a held session: a prompt sent "now", a typed action
// and an action's keys are refused with the reason; a queued prompt waits
// and goes out once the phone lets go. Interrupt keys still go through:
// stopping the agent matters more than keeping the screen.
//
// Every typing path checks under the session's typing lock, right before
// its keys. Whoever takes the terminal natively takes the same lock around
// the moment it does (LockTyping), so the check and the keys cannot have a
// take-over between them.

// heldNatively is the error for a session a phone holds natively, or nil.
func (e *Engine) heldNatively(s *sess) *harness.FallbackError {
	if e.drivingMenu(s.id) {
		return &harness.FallbackError{Reason: "source", Detail: s.ad.Name + " has a menu open that the phone is using. Nothing was typed: finish or close it first."}
	}
	if panel, open := e.menuLeftOpen(s); open {
		name := panel
		if name == "" {
			name = "a menu"
		} else {
			name = "“" + name + "”"
		}
		return &harness.FallbackError{Reason: "source", Detail: s.ad.Name + " still has " + name + " open in its terminal. Nothing was typed: close it there first."}
	}
	if e.cfg.SurfaceHeld == nil || !e.cfg.SurfaceHeld(s.id) {
		return nil
	}
	return &harness.FallbackError{Reason: "source", Detail: s.ad.Name + " is showing a screen on the phone. Nothing was typed: finish or close it first."}
}

// LockTyping takes the lock every typing path of the session holds while it
// types, and returns its release. Code that takes the session's terminal
// away from the keyboard (a native screen opening) holds it while it does,
// so no keys are on their way at that moment and none start until it is
// marked held.
func LockTyping(session string) (unlock func()) {
	l := typingLock(session)
	l.Lock()
	return l.Unlock
}
