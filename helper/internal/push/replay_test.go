package push

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Replays every recorded session stream in protocol/v1/replays through the
// push sender, as if the session were not muted and no app were in the
// foreground, and checks what it would have sent: every notification
// matches what happened (no false "needs you", no "finished" for work that
// goes on, nothing for a request already answered) and every Live Activity
// carries its context. The same stream with the session muted, as it was
// recorded, must alert nothing.

const replayDir = "../../../protocol/v1/replays"

// replayKnown lists product bugs the replays expose: fixture → rule →
// reason. They are logged, not failed; a listed rule that no longer breaks
// fails, so the entry is removed once the product is fixed.
var replayKnown = map[string]map[string]string{
	"omp.turn": {
		"false-needs-you": "omp rings the terminal bell mid-turn; with no prompt on screen the watcher still sends a needs_you notify titled \"Bell\" (watch.go), a false \"needs you\" while the agent works.",
	},
	"claude.subagent": {
		"done-then-working": "Claude's Stop hook fires when the main agent hands off to a background subagent, so the phone is told \"Claude finished\" while the turn goes on.",
	},
}

type replayRecord struct {
	T      int64           `json:"t"`
	Dir    string          `json:"dir"`
	Method string          `json:"method"`
	Frame  json.RawMessage `json:"frame"`
}

func loadReplay(t *testing.T, path string) []replayRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
	var out []replayRecord
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		var r replayRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out = append(out, r)
	}
	return out
}

// sent is one push the relay received, decoded.
type sent struct {
	at    int64 // fixture ms of the event that caused it
	alert *wire.PushPlaintext
	live  *liveActivity
	ctx   *LiveContext
}

// replayRun is what the sender did with one fixture.
type replayRun struct {
	pushes []sent
	// per event, in order, the facts the rules need
	facts []replayFacts
}

type replayFacts struct {
	t       int64
	kind    string
	level   string
	rid     string
	status  string
	pending int
	working bool
	sent    bool // the app sent a prompt since the previous event
	dialog  bool // a terminal dialog notice is on screen
}

func runReplay(t *testing.T, records []replayRecord, muted bool) replayRun {
	t.Helper()
	e := newEnv(t, 0)
	e.pairWith(t, "dev_a", func(p *wire.PairParams) { p.LiveActivityStartToken = startToken })
	name := ""
	e.sender.Session = func(id string) (SessionInfo, bool) {
		bib, agent := "01", "claude"
		return SessionInfo{Bib: &bib, Agent: &agent, Muted: muted, Name: name}, true
	}
	var run replayRun
	var base time.Time
	// A dialog's notice can land just after the notify it caused.
	var dialogs []int64
	for _, r := range records {
		if r.Method == "harness" {
			if _, d := harnessFacts(r.Frame, false, false); d {
				dialogs = append(dialogs, r.T)
			}
		}
	}
	pending := map[string]bool{}
	status, working, sentPrompt, dialog := "", false, false, false
	seen := 0
	for _, r := range records {
		if r.Dir == "out" && r.Method == "harness.send" {
			sentPrompt = true
		}
		if r.Method == "harness" {
			working, dialog = harnessFacts(r.Frame, working, dialog)
			continue
		}
		if r.Method != "event" {
			continue
		}
		var f struct {
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(r.Frame, &f); err != nil {
			t.Fatal(err)
		}
		p := f.Params
		if s, ok := p["session"].(map[string]any); ok {
			s["muted"] = muted
			if n, ok := s["name"].(string); ok {
				name = n
			}
			status, _ = s["status"].(string)
		}
		raw, _ := json.Marshal(p)
		var ev wire.Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("event: %v", err)
		}
		if base.IsZero() {
			base = time.Unix(time.Now().Unix(), 0)
			if ts, err := time.Parse(time.RFC3339, fmt.Sprint(p["ts"])); err == nil {
				base = ts
			}
		}
		e.clock = base.Add(time.Duration(r.T) * time.Millisecond)
		kind, _ := p["kind"].(string)
		soon := false
		for _, d := range dialogs {
			soon = soon || (d >= r.T && d-r.T <= 1500)
		}
		fact := replayFacts{t: r.T, kind: kind, status: status, working: working, sent: sentPrompt, dialog: dialog || soon}
		switch kind {
		case "approval.open":
			a, _ := p["approval"].(map[string]any)
			fact.rid, _ = a["requestId"].(string)
			pending[fact.rid] = true
		case "approval.closed":
			fact.rid, _ = p["requestId"].(string)
			delete(pending, fact.rid)
		case "notify":
			fact.level, _ = p["level"].(string)
		}
		fact.pending = len(pending)
		sentPrompt = false
		e.handle(ev)
		e.tick()
		run.facts = append(run.facts, fact)
		seen = collect(t, e, &run, seen, r.T)
	}
	// Let coalesced Live Activity updates and the quiet end go out.
	for i := 0; i < 20; i++ {
		e.clock = e.clock.Add(30 * time.Second)
		e.tick()
	}
	collect(t, e, &run, seen, -1)
	return run
}

// harnessFacts follows the mission's working flag and terminal dialogs
// (fallback notices) through a harness notification.
func harnessFacts(frame json.RawMessage, working, dialog bool) (bool, bool) {
	var f struct {
		Params struct {
			Items []struct {
				Kind   string `json:"kind"`
				Notice *struct {
					Code string `json:"code"`
				} `json:"notice"`
			} `json:"items"`
			Mission *struct {
				Ops []struct {
					Op      string `json:"op"`
					Summary *struct {
						Working bool `json:"working"`
					} `json:"summary"`
				} `json:"ops"`
			} `json:"mission"`
		} `json:"params"`
	}
	if json.Unmarshal(frame, &f) != nil {
		return working, dialog
	}
	for _, it := range f.Params.Items {
		if it.Kind == "notice" && it.Notice != nil {
			dialog = it.Notice.Code == "fallback"
		} else if it.Kind == "user" || it.Kind == "assistant" || it.Kind == "tool" {
			dialog = false
		}
	}
	if f.Params.Mission != nil {
		for _, op := range f.Params.Mission.Ops {
			if op.Op == "summary" && op.Summary != nil {
				working = op.Summary.Working
			}
		}
	}
	return working, dialog
}

func collect(t *testing.T, e *env, run *replayRun, seen int, at int64) int {
	t.Helper()
	got := e.relay.received()
	for _, p := range got[seen:] {
		s := sent{at: at}
		switch {
		case p.Kind == relayLiveActivity && p.LiveActivity != nil:
			la := *p.LiveActivity
			s.live = &la
			if la.ContentState.Ctx != nil {
				c := e.liveCtx(t, p, "dev_a")
				s.ctx = &c
			}
		case p.Ciphertext != "":
			pt := e.open(t, p.Ciphertext, "dev_a")
			s.alert = &pt
		}
		run.pushes = append(run.pushes, s)
	}
	return len(got)
}

// replayRules checks one unmuted run. Each violation is "rule: detail".
func replayRules(run replayRun) []string {
	var out []string
	cleared := map[string]bool{}
	for _, p := range run.pushes {
		if p.alert == nil {
			continue
		}
		a := p.alert
		switch a.Kind {
		case wire.PushClear:
			cleared[a.RequestID] = true
		case wire.PushNeedsYou:
			if cleared[a.RequestID] {
				out = append(out, fmt.Sprintf("needs-you-after-clear: %s alerted again at %dms after it was cleared", a.RequestID, p.at))
			}
		}
	}
	for i, f := range run.facts {
		if f.kind != "notify" {
			continue
		}
		switch f.level {
		case "needs_you":
			// "Waiting for your input" is true only while something waits.
			if f.pending == 0 && f.status != "needs_you" && !f.dialog {
				out = append(out, fmt.Sprintf("false-needs-you: a needs-you notification at %dms with nothing waiting (session %s)", f.t, f.status))
			}
		case "done":
			if f.pending > 0 {
				out = append(out, fmt.Sprintf("done-while-waiting: \"finished\" at %dms with %d requests pending", f.t, f.pending))
			}
			for _, g := range run.facts[i+1:] {
				if g.t-f.t > 5000 || g.sent {
					break
				}
				if g.kind == "session" && g.status == "working" {
					out = append(out, fmt.Sprintf("done-then-working: \"finished\" at %dms, working again at %dms without a new prompt", f.t, g.t))
					break
				}
			}
		}
	}
	for _, p := range run.pushes {
		if p.live == nil || p.live.Event == liveEnd {
			continue
		}
		cs := p.live.ContentState
		if p.ctx == nil || p.ctx.Title == "" {
			out = append(out, fmt.Sprintf("live-no-title: Live Activity %s (%s) at %dms carries no session title", p.live.Event, cs.Status, p.at))
		}
		if cs.Status == "needs_you" {
			if cs.RequestID == nil || cs.Waiting < 1 {
				out = append(out, fmt.Sprintf("live-needs-you-empty: needs_you at %dms with waiting=%d and no request", p.at, cs.Waiting))
			} else if p.ctx == nil || p.ctx.Ask == "" {
				out = append(out, fmt.Sprintf("live-no-ask: needs_you at %dms without the request's question", p.at))
			}
		}
	}
	return out
}

func TestReplayPushes(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(replayDir, "*.jsonl"))
	if len(files) == 0 {
		t.Skip("no replay fixtures")
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		t.Run(name, func(t *testing.T) {
			records := loadReplay(t, path)
			// As recorded: muted, so nothing but clears may alert.
			for _, p := range runReplay(t, records, true).pushes {
				if p.alert != nil && p.alert.Kind != wire.PushClear {
					t.Errorf("muted session alerted: %s %q at %dms", p.alert.Kind, p.alert.Title, p.at)
				}
				if p.live != nil && p.live.Event != liveEnd {
					t.Errorf("muted session drove the Live Activity: %s at %dms", p.live.ContentState.Status, p.at)
				}
			}
			broken := map[string][]string{}
			for _, v := range replayRules(runReplay(t, records, false)) {
				rule, _, _ := strings.Cut(v, ":")
				broken[rule] = append(broken[rule], v)
			}
			known := replayKnown[name]
			rules := make([]string, 0, len(broken))
			for r := range broken {
				rules = append(rules, r)
			}
			sort.Strings(rules)
			for _, rule := range rules {
				if why, ok := known[rule]; ok {
					t.Logf("known product bug (%s): %s — %s", rule, broken[rule][0], why)
					continue
				}
				t.Errorf("%s", broken[rule][0])
			}
			for rule, why := range known {
				if why != "" && len(broken[rule]) == 0 { // a known bug that went away
					t.Errorf("known bug %s no longer reproduces; remove it from replayKnown", rule)
				}
			}
		})
	}
}
