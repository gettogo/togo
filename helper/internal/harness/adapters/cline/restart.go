package cline

import (
	"encoding/json"
	"io"
	"os"
	"syscall"

	"github.com/gettogo/togo/helper/internal/harness/source"
)

// The session the chat follows is learned from the documents Cline
// writes and from relaunches. After a helper restart every document of
// the folder is read again in no set order, and the first one taken could
// be a conversation the pane has left. The followed session is kept under
// the helper's run folder, run/follow-<session>.cline.json (0600), and
// taken back when the mapper is built again.
//
// Set: whenever the chat locks onto a session (its first document, a move
// to a newer session of this pane, a resume relaunch). Cleared: with the
// session (Engine.Forget, source.RemoveFollows). A relaunch onto another
// session replaces it; one that starts a new session keeps it until that
// session's document moves the chat. Kept on turn ends, interrupts,
// reconnects and helper restarts.
const followName = "cline"

// restore locks onto the session followed before a helper restart, when
// it is still a session of this folder. Its document's updated_at is read
// back too, so an older document of another session read after the
// restart is not taken for a newer one.
func (m *messages) restore() {
	id := source.LoadFollowed(m.env, followName)
	if !sessionID.MatchString(id) || !m.started(id) {
		return
	}
	m.lock(id)
	m.updated = updatedAt(m.state["transcript_path"])
}

// started reports whether the session's manifest names this folder.
func (m *messages) started(id string) bool {
	mf, ok := m.manifest(id)
	return ok && sameDir(mf.Cwd, m.env.Cwd)
}

// updatedAt reads the updated_at field that leads a messages document,
// without reading the messages after it. "" when it cannot.
func updatedAt(p string) string {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	dec := json.NewDecoder(io.LimitReader(f, 1<<20))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return ""
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return ""
		}
		if k == "updated_at" {
			var v string
			if dec.Decode(&v) != nil {
				return ""
			}
			return v
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return ""
		}
	}
	return ""
}
