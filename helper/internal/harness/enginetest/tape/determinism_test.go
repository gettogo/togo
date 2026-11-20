package tape

import (
	"bytes"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// The same scenario and seed give the same tape, byte for byte: what a
// run stamps (epochs, the notification clock, ids drawn at random, the
// scratch folders) is pinned or normalised. One agent of each way the
// engine reads a session is enough here; the matrix is compared by the
// command (see the tests' notes in cmd/togo-tape).
// unordered are pairs whose tape holds what the engine reports in an order
// that varies between runs, not a field to normalise: the tape is a
// faithful record of one run.
var unordered = map[string]string{
	"question-answered/crush": "Crush's database is polled: the answered question and the reply it follows arrive as two notifications in either order",
	"subagents/omp":           "omp's subagent files are found on a poll: a history reset and the agent rows arrive in either order",
	"subagents/claude":        "the engine reads Claude's main file and its subagents' files on separate feeds: a history reset and the agent rows arrive in either order",
	// Copilot's replies come from a log the engine finds on a poll, after
	// the hook calls: whether the replies are in the chat, and so every
	// Seq after them, depends on when the poll ran.
	"approval-answered/copilot":  "Copilot's replies are read from a polled log: their Seq varies",
	"question-answered/copilot":  "Copilot's replies are read from a polled log: their Seq varies",
	"send-while-working/copilot": "Copilot's replies are read from a polled log: their Seq varies",
	// opencode2's server brings the session as a stream that is written in
	// pieces while the helper reads it: how many Seq values the first page
	// spans (cursor 501 to 504 seen) depends on how the pieces were read.
	"long-chat-restart/opencode2": "opencode2's session arrives as a stream read in pieces: the cursor of the first page varies",
	"interrupt/copilot":           "Copilot's replies are read from a polled log: their Seq varies",
}

func TestSameScenarioSameTape(t *testing.T) {
	for _, agent := range deterministicAgents() {
		for _, sc := range Catalogue() {
			t.Run(sc.ID+"/"+agent, func(t *testing.T) {
				t.Parallel()
				if why := sc.Applies(agent); why != "" {
					t.Skip(why)
				}
				if hookAgent(agent) && sc.ID != "long-chat-restart" {
					t.Skip("hook-fed agents: the engine reads their hook calls and polled logs after the app has opened the session, so how much of the live turn the open holds varies")
				}
				if why := unordered[sc.ID+"/"+agent]; why != "" {
					t.Skip(why)
				}
				support.Heavy(t)
				var tapes [2]*Tape
				for i := range tapes {
					var tp *Tape
					var err error
					if i == 0 {
						// The first run is the one the validity test uses; the
						// second must not overlap it (same session id).
						tp, err = export(t, sc, agent)
					} else {
						tp, err = Export(t, sc, agent)
					}
					if sk, ok := err.(Skip); ok {
						t.Skip(string(sk))
					}
					if err != nil {
						t.Fatal(err)
					}
					tapes[i] = tp
				}
				a, b := tapes[0].Bytes(), tapes[1].Bytes()
				if !bytes.Equal(a, b) && sc.ID == "send-while-working" && bytes.Equal(withoutNoReply(tapes[0].Lines), withoutNoReply(tapes[1].Lines)) {
					// Whether the queued prompt gets a "no reply" notice
					// depends on how long typing it took:
					// TestKnownFault_QueuedPromptJudgedByThePreviousTurnsEnd (e2e/generic).
					t.Logf("the tapes differ only in the no-reply notice of the known fault")
					a = b
				}
				if !bytes.Equal(a, b) {
					t.Errorf("two exports differ (%d and %d bytes): first difference at line %d:\n%s", len(a), len(b), firstDiff(tapes[0].Lines, tapes[1].Lines), around(tapes[0].Lines, tapes[1].Lines))
				}
				if x, y := tapes[0].Manifest, tapes[1].Manifest; x.SessionID != y.SessionID || x.ScenarioID != y.ScenarioID {
					t.Errorf("manifests differ: %+v %+v", x, y)
				}
			})
		}
	}
}

// deterministicAgents are compared in the default run; the long run
// compares every profile.
func deterministicAgents() []string {
	if support.LongMode() {
		return sessiongen.Names()
	}
	return []string{"omp", "crush", "opencode", "opencode2", "codex"}
}

// withoutNoReply is the tape without the lines that carry the engine's
// "ended the turn without a reply" notice.
func withoutNoReply(lines [][]byte) []byte {
	var out [][]byte
	for _, l := range lines {
		if !bytes.Contains(l, []byte(`"id":"noreply-`)) {
			out = append(out, l)
		}
	}
	return bytes.Join(out, []byte("\n"))
}

func firstDiff(a, b [][]byte) int {
	for i := range min(len(a), len(b)) {
		if !bytes.Equal(a[i], b[i]) {
			return i + 1
		}
	}
	return min(len(a), len(b)) + 1
}

// around shows the bytes where the first differing lines part.
func around(a, b [][]byte) string {
	i := firstDiff(a, b) - 1
	if i >= len(a) || i >= len(b) {
		return "(one tape is longer)"
	}
	x, y := a[i], b[i]
	j := 0
	for j < len(x) && j < len(y) && x[j] == y[j] {
		j++
	}
	cut := func(s []byte) string { return string(s[max(0, j-100):min(len(s), j+100)]) }
	return cut(x) + "\n" + cut(y)
}
