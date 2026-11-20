package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The other ways omp changes conversation under a session. /new, /fork
// and a pick in omp's own /resume chooser run in the same omp process, so
// the pane keeps its device and omp rewrites that device's record. A
// relaunch after omp exited respawns the pane, as a resume from the phone
// does.

// A new conversation started from the phone (/new): omp writes a new file
// and names it in the same device's record.
func TestOmpNewConversationFromPhoneMovesTheChat(t *testing.T) {
	r := newOmpRig(t, "s_ompneigh01")
	r.record("/dev/ttys011", ompConvAFile)
	r.start()
	r.waitShowsOnly(ompConvAText, ompConvBText)
	// The phone types /new; omp starts the new file.
	r.record("/dev/ttys011", ompConvBFile)
	r.waitShowsOnly(ompConvBText, ompConvAText)
}

// A fork from the phone (/fork): omp writes a new main conversation whose
// header names the one it came from (seen on 18.20). The chat shows the
// fork as the conversation, not as a subagent of the old one.
func TestOmpForkFromPhoneMovesTheChat(t *testing.T) {
	r := newOmpRig(t, "s_ompneigh02")
	r.record("/dev/ttys011", ompConvAFile)
	r.start()
	r.waitShowsOnly(ompConvAText, ompConvBText)
	b, err := os.ReadFile(filepath.Join("..", "adapters", "omp", "testdata", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	header := []byte(`"cwd":"/Users/dev/project"}`)
	if !bytes.Contains(b, header) {
		t.Fatal("fixture header changed")
	}
	const fork = "2026-11-06T10-05-00-000Z_0b7c1f2a9e.jsonl"
	r.writeConversation(fork, bytes.Replace(b, header,
		[]byte(`"cwd":"/Users/dev/project","parentSession":"`+filepath.Join(r.dir, ompConvAFile)+`"}`), 1))
	// The phone types /fork.
	r.record("/dev/ttys011", fork)
	r.waitShowsOnly(ompConvBText, ompConvAText)
}

// A conversation picked in omp's own /resume chooser at the desk.
func TestOmpResumePickedInOmpsChooserMovesTheChat(t *testing.T) {
	r := newOmpRig(t, "s_ompneigh03")
	r.record("/dev/ttys011", ompConvAFile)
	r.start()
	r.waitShowsOnly(ompConvAText, ompConvBText)
	r.record("/dev/ttys011", ompConvBFile)
	r.waitShowsOnly(ompConvBText, ompConvAText)
}

// omp exited and the phone ran it again (session.relaunch): the pane is
// respawned on a new device, where the new omp names its conversation.
// The old device's record is still on disk.
func TestOmpRelaunchAfterExitFollowsTheNewRun(t *testing.T) {
	r := newOmpRig(t, "s_ompneigh04")
	r.record("/dev/ttys011", ompConvAFile)
	r.next = []string{"/dev/ttys012"}
	r.start()
	r.waitShowsOnly(ompConvAText, ompConvBText)
	r.setStatus("ended")
	r.respawn([]string{"omp"})
	r.record("/dev/ttys012", ompConvBFile)
	r.setStatus("idle")
	r.waitShowsOnly(ompConvBText, ompConvAText)
}
