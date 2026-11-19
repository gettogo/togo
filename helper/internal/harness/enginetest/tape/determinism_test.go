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
	"subagents/omp":    "omp's subagent files are found on a poll: a history reset and the agent rows arrive in either order",
	"subagents/claude": "the engine reads Claude's main file and its subagents' files on separate feeds: a history reset and the agent rows arrive in either order",
	// Copilot's replies come from a log the engine finds on a poll, after
	// the hook calls: whether the replies are in the chat, and so every
	// Seq after them, depends on when the poll ran.
	"approval-answered/copilot":  "Copilot's replies are read from a polled log: their Seq varies",
	"question-answered/copilot":  "Copilot's replies are read from a polled log: their Seq varies",
	"send-while-working/copilot": "Copilot's replies are read from a polled log: their Seq varies",
	"interrupt/copilot":          "Copilot's replies are read from a polled log: their Seq varies",
}

func TestSameScenarioSameTape(t *testing.T) {
	for _, agent := range deterministicAgents() {
		for _, sc := range Catalogue() {
			t.Run(sc.ID+"/"+agent, func(t *testing.T) {
				t.Parallel()
				if why := sc.Applies(agent); why != "" {
					t.Skip(why)
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
				if !bytes.Equal(a, b) {
					t.Errorf("two exports differ (%d and %d bytes): first difference at line %d", len(a), len(b), firstDiff(tapes[0].Lines, tapes[1].Lines))
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

func firstDiff(a, b [][]byte) int {
	for i := range min(len(a), len(b)) {
		if !bytes.Equal(a[i], b[i]) {
			return i + 1
		}
	}
	return min(len(a), len(b)) + 1
}
