package codex

import (
	"regexp"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/harness/source/rpc"
)

// The app-server keeps every thread the TUI opened loaded, so after a /new
// or /resume at the desk it lists two top-level threads, and after a
// helper restart the feed joins both with nothing to say which one the
// TUI shows. The main thread is remembered under the helper's run folder,
// run/follow-<session>.codex.json (0600), and preferred when it is listed
// again, until a turn on another thread moves the chat.
//
// Set: whenever the main thread changes (setMain: the first thread
// joined, a /new or /resume at the desk, a resume from the phone).
// Cleared: with the session (Engine.Forget, source.RemoveFollows). A
// relaunch starts a new app-server, which does not list the old thread,
// so it no longer matters; the new main thread replaces it. Kept on turn
// ends, interrupts, reconnects and helper restarts.
const followName = rpc.FollowedThread

// threadID is the shape of a Codex thread id (a UUID).
var threadID = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

func remembered(env harness.Env) string {
	if id := source.LoadFollowed(env, followName); threadID.MatchString(id) {
		return id
	}
	return ""
}

func (a *AppServer) remember(thread string) {
	if thread != a.saved && threadID.MatchString(thread) {
		a.saved = thread
		source.SaveFollowed(a.env, followName, thread)
	}
}

// takesMain says whether a top-level thread joined from the server's list
// becomes the main thread. After /new or /resume at the desk several are
// loaded and the TUI is on one of them: the one followed before a helper
// restart wins over an idle one; else the first stays unless a later one
// is active, or the current one is idle and the later one saw more recent
// activity. A thread that starts a turn takes over in turnStarted.
func (a *AppServer) takesMain(r resumeResult) bool {
	if a.main == "" || a.main == r.Thread.ID {
		return true
	}
	if r.Thread.Status.Type == "active" {
		return true
	}
	if a.follow != "" && a.main != a.follow && r.Thread.ID == a.follow {
		return a.state["working"] != "true"
	}
	if a.follow != "" && a.main == a.follow {
		return false
	}
	return a.state["working"] != "true" && freshness(r).After(a.mainFresh)
}

// freshness is the start of a thread's latest turn, else its creation.
func freshness(r resumeResult) time.Time {
	t := secs(r.Thread.CreatedAt)
	for _, turn := range r.Thread.Turns {
		if s := secs(turn.StartedAt); s.After(t) {
			t = s
		}
	}
	return t
}

// otherConversation records that subagent thread id was spawned from
// thread up, and reports whether its top-level thread is known and is not
// the main one: a subagent of the conversation the desk left, whose
// history is not this chat's either. Nothing of such a thread is
// described to the mapper, so its later events draw nothing.
func (a *AppServer) otherConversation(id, up string) bool {
	if a.ups == nil {
		a.ups = map[string]string{}
	}
	if len(a.ups) < 4096 {
		a.ups[id] = up
	}
	top := up
	for range 16 {
		next, ok := a.ups[top]
		if !ok {
			break
		}
		top = next
	}
	t := a.threads[top]
	return t != nil && t.top && a.main != "" && top != a.main
}

// resumedAtDesk marks the main thread, loaded by the server after the feed
// first asked, as fresh as of its resume: the conversation the desk left,
// listed again after a reconnect, does not take the chat back.
func (a *AppServer) resumedAtDesk(at time.Time) {
	a.follow = ""
	if at.After(a.mainFresh) {
		a.mainFresh = at
	}
}
