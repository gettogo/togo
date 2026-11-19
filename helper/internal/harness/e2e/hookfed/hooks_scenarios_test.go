package hookfed

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// The scenarios of the hook-driven agents. Each takes the agent's
// description (hooks_expect_test.go) and runs the real engine over the
// calls its profile wrote, delivered as the hook processes would.

// mixed is a script of n turns with every feature the agent has: tool
// steps, failed tools, permission, questions, subagents, a steered
// prompt, an interrupt, a provider error and a new conversation.
func mixed(a hookAgent, n int) []sessiongen.Turn {
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		t := sessiongen.Turn{Say: 200, Tools: 1 + i%3, Output: 300, Hook: &sessiongen.HookTurn{}}
		if i%17 == 5 {
			t.Hook.Fail = 1
		}
		if a.canApprove && i%11 == 3 {
			t.Hook.Approve = 1
		}
		if a.canAsk && i%30 == 12 {
			t.Hook.Ask = &sessiongen.HookAsk{Prompt: "Which one?", Options: []string{"Red", "Green"}, Answer: "Green"}
		}
		if a.canAgents && i%25 == 10 {
			t.Agents = &sessiongen.Batch{Size: 2 + i%2}
		}
		if a.canAgents && i%50 == 30 {
			t.Agents = &sessiongen.Batch{Size: 2, Background: true}
		}
		// Copilot's mapper waits up to 1.5 s at a turn end that has no
		// final message (hookstyle/mapper.go replyWaitStop), so it gets
		// these seldom to keep the run short.
		switch {
		case !a.slowStop && i%50 == 20, a.slowStop && i%400 == 399:
			t.Interrupt = true
		case !a.slowStop && i%70 == 33, a.slowStop && i%500 == 499:
			t.Error = true
		}
		if i%40 == 7 {
			t.Hook.Steer = true
		}
		if i%100 == 60 {
			t.New = true
		}
		turns[i] = t
	}
	return turns
}

func write(t *testing.T, e support.Env, a hookAgent, turns []sessiongen.Turn) *sessiongen.Layout {
	t.Helper()
	l, err := sessiongen.Write(e.Home, a.profile(), sessiongen.Script{Seed: 21, Start: e.Start, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// drain waits until the engine has read what was delivered: its newest
// Seq stays put for a while.
func drain(r *enginetest.Rig) {
	prev, stable := r.Cursor(), 0
	for stable < 3 {
		time.Sleep(10 * time.Millisecond)
		if c := r.Cursor(); c == prev {
			stable++
		} else {
			prev, stable = c, 0
		}
	}
}

// run delivers calls in bursts the engine can keep up with (a feed that
// cannot loses payloads rather than block a hook).
func run(t testing.TB, r *enginetest.Rig, a hookAgent, calls []sessiongen.HookCall) {
	t.Helper()
	for len(calls) > 0 {
		n := min(len(calls), 100)
		support.Deliver(t, r, a.name, calls[:n]...)
		drain(r)
		calls = calls[n:]
	}
}

// longScenario is a session of n turns: the chat keeps the newest 2000
// items, the last turns are exact, and every rule holds.
func longScenario(t *testing.T, a hookAgent, n int) {
	e := support.NewEnv(t, 48*time.Hour)
	turns := mixed(a, n)
	l := write(t, e, a, turns)
	r := e.RigWith(t, enginetest.Options{Agent: a.name, NoRedact: true})
	run(t, r, a, l.Hooks)
	st := settled(r, lastPrompt(a, l.Hooks))
	exp := expected(a, l.Hooks)
	verify(t, "long", a, st, exp, len(exp)-min(len(exp), 80))
	checkHook(t, "long", a, st, l, true)
	enginetest.Fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(7)))
	if len(st.Items) > 2000 || len(st.Items) < 500 {
		t.Errorf("%d items kept, want between 500 and 2000", len(st.Items))
	}
	if sum := st.Mission.Summary; sum.Working || sum.Agents != 0 || sum.NeedsYou != 0 {
		t.Errorf("summary after the last turn: %+v", sum)
	}
	if n >= 300 && st.Discarded == 0 {
		t.Errorf("a session of %d turns discarded nothing", n)
	}
}

// restartScenario restarts the engine at seeded points of a session of 60
// turns: between turns, in the middle of one, and while the agent waits
// (an approval, a question or a running tool). The hook records are kept
// under the helper's run folder, so each restart gives back the chat it
// had (enginetest.Restarted), sound and with the same summary, and after
// the last turn every turn is exact and nothing is left running.
func restartScenario(t *testing.T, a hookAgent) []string {
	e := support.NewEnv(t, time.Hour)
	l := write(t, e, a, mixed(a, 60))
	exp := expected(a, l.Hooks)
	cuts := restartPoints(a, l.Hooks)
	r := e.RigWith(t, enginetest.Options{Agent: a.name, NoRedact: true})
	done, kinds := 0, []string{}
	for _, c := range cuts {
		run(t, r, a, l.Hooks[done:c.at])
		done = c.at
		drain(r)
		before := r.Read()
		r.Restart()
		after := r.Read()
		if len(before.Items) == 0 {
			t.Fatalf("restart %s at %d: nothing was drawn before it", c.kind, c.at)
		}
		support.Fail(t, "restart "+c.kind, enginetest.Restarted(before, after), l)
		checkHook(t, "restart "+c.kind, a, after, l, false)
		kinds = append(kinds, c.kind)
	}
	run(t, r, a, l.Hooks[done:])
	st := settled(r, lastPrompt(a, l.Hooks))
	checkHook(t, "after restarts", a, st, l, true)
	verify(t, "after restarts", a, st, exp, 0)
	if sum := st.Mission.Summary; sum.Working || sum.Agents != 0 || sum.NeedsYou != 0 {
		t.Errorf("summary after the last turn: %+v", sum)
	}
	return kinds
}

type cut struct {
	at   int
	kind string
}

// restartPoints picks, from the seed, five places in the calls: two
// between turns, two inside a turn after a tool began, one while the agent
// waits for the user (a permission or a question; a running tool where the
// agent has neither).
func restartPoints(a hookAgent, calls []sessiongen.HookCall) []cut {
	var idle, mid, wait []int
	open := false
	for i, c := range calls {
		if i < 12 || i > len(calls)-12 {
			continue
		}
		for _, k := range a.classify(c) {
			switch k.what {
			case "prompt":
				open = true
			case "tool":
				if open {
					mid = append(mid, i+1)
				}
			case "ask":
				if open {
					wait = append(wait, i+1)
				}
			}
		}
		if a.waitEvent != "" && c.Event == a.waitEvent && open {
			wait = append(wait, i+1)
		}
		if c.End != "" {
			idle = append(idle, i+1)
			open = false
		}
	}
	pick := func(from []int, k int, kind string) cut {
		return cut{from[(k*7919)%len(from)], kind}
	}
	if len(wait) == 0 {
		wait = mid
	}
	cs := []cut{pick(idle, 1, "between turns"), pick(mid, 2, "mid-turn"), pick(wait, 3, "waiting"), pick(mid, 5, "mid-turn"), pick(idle, 8, "between turns")}
	// In order, no two at one place.
	for i := range cs {
		for j := i + 1; j < len(cs); j++ {
			if cs[j].at < cs[i].at {
				cs[i], cs[j] = cs[j], cs[i]
			}
		}
	}
	for i := 1; i < len(cs); i++ {
		if cs[i].at <= cs[i-1].at {
			cs[i].at = cs[i-1].at + 1
		}
	}
	return cs
}

// restartKeepsChat restarts the helper after a few turns of a
// hook-driven agent: the chat after the restart is the chat before it.
func restartKeepsChat(t *testing.T, a hookAgent) {
	e := support.NewEnv(t, time.Hour)
	l := write(t, e, a, mixed(a, 4))
	r := e.Rig(t, a.name)
	run(t, r, a, l.Hooks)
	before := settled(r, lastPrompt(a, l.Hooks))
	if len(before.Items) == 0 {
		t.Fatal("nothing was drawn before the restart")
	}
	r.Restart()
	after := r.SettleUntil(10*time.Second, func(st enginetest.State) bool { return len(st.Items) >= len(before.Items) })
	if len(after.Items) != len(before.Items) {
		t.Errorf("%d items after the restart, %d before", len(after.Items), len(before.Items))
	}
	support.Fail(t, "restart", enginetest.Restarted(before, after), l)
}

// newConversation runs two conversations and a resume of the first.
func newConversation(t *testing.T, a hookAgent, resume bool) enginetest.State {
	e := support.NewEnv(t, time.Hour)
	turns := []sessiongen.Turn{
		{Say: 30, Tools: 2, Output: 50},
		{Say: 30, Tools: 1, Output: 50, New: true},
		{Say: 30, Tools: 1, Output: 50},
	}
	if resume {
		turns = append(turns, sessiongen.Turn{Say: 30, Tools: 1, Output: 50, Hook: &sessiongen.HookTurn{Resume: true}})
	}
	l := write(t, e, a, turns)
	r := e.Rig(t, a.name)
	run(t, r, a, l.Hooks)
	st := settled(r, lastPrompt(a, l.Hooks))
	exp := expected(a, l.Hooks)
	if len(exp) != len(turns) {
		t.Fatalf("%d turns expected, want %d", len(exp), len(turns))
	}
	verify(t, "conversations", a, st, exp, 0)
	checkHook(t, "conversations", a, st, l, true)
	return st
}

// exitAndRelaunch ends the agent in the middle of a turn that has
// subagents running, then starts it again in a new conversation.
func exitAndRelaunch(t *testing.T, a hookAgent) {
	e := support.NewEnv(t, time.Hour)
	turns := []sessiongen.Turn{{Say: 30, Tools: 2, Output: 50}, {Say: 30, Tools: 2, Output: 50}}
	if a.canAgents {
		turns[1].Agents = &sessiongen.Batch{Size: 2, Background: true}
	}
	turns = append(turns, sessiongen.Turn{Say: 30, Tools: 1, Output: 50, New: true})
	l := write(t, e, a, turns)
	r := e.Rig(t, a.name)
	// Up to the third turn's start, and the second turn's end, where an
	// agent with subagents has them running.
	ends := 0
	cut := len(l.Hooks)
	for i, c := range l.Hooks {
		if c.End != "" {
			ends++
			if ends == 2 {
				cut = i + 1
			}
		}
	}
	if a.canAgents {
		cut-- // the second turn's end is not delivered: the agent dies first
	}
	run(t, r, a, l.Hooks[:cut])
	r.SetStatus("working")
	mid := r.SettleUntil(10*time.Second, nil)
	if a.canAgents {
		if mid.Mission.Summary.Agents == 0 {
			t.Fatalf("no subagent runs before the agent exits: %+v", mid.Mission.Summary)
		}
	}
	r.SetEnded(true)
	gone := r.SettleUntil(10*time.Second, func(st enginetest.State) bool { return !st.Mission.Summary.Working && st.Mission.Summary.Agents == 0 })
	ex := *l
	ex.Truth.Exited = true
	checkHook(t, "after the exit", a, gone, &ex, true)
	notice := false
	for _, it := range gone.Items {
		notice = notice || it.Notice != nil
	}
	if !notice {
		t.Errorf("the chat does not say the agent exited")
	}
	for _, it := range gone.Items {
		if it.Tool != nil && support.Running(it.Tool.State) {
			t.Errorf("tool %s is %s after the agent exited", it.ID, it.Tool.State)
		}
	}
	// Relaunch: a new conversation, and its turn runs and ends.
	r.SetEnded(false)
	rest := l.Hooks[cut:]
	var third []sessiongen.HookCall
	for i, c := range rest {
		for _, k := range a.classify(c) {
			if k.what == "prompt" && i >= 0 {
				third = rest[startOfNewConversation(a, rest):]
			}
		}
		if third != nil {
			break
		}
	}
	run(t, r, a, third)
	st := settled(r, lastPrompt(a, third))
	exp := expected(a, third)
	verify(t, "after relaunch", a, st, exp, 0)
	checkHook(t, "after relaunch", a, st, l, true)
	if sum := st.Mission.Summary; sum.Working || sum.Agents != 0 {
		t.Errorf("summary after the relaunched turn: %+v", sum)
	}
}

// startOfNewConversation is the index of the first call of the last
// conversation: the start call before the final prompt.
func startOfNewConversation(a hookAgent, calls []sessiongen.HookCall) int {
	last := 0
	for i, c := range calls {
		if c.Event == a.startEvent {
			last = i
		}
	}
	return last
}

// interrupted is a turn stopped by the user in the middle of its tools,
// with subagents running, followed by another turn.
func interrupted(t *testing.T, a hookAgent) {
	e := support.NewEnv(t, time.Hour)
	first := sessiongen.Turn{Say: 30, Tools: 3, Output: 50, Interrupt: true}
	if a.canAgents {
		first.Agents = &sessiongen.Batch{Size: 2, Background: true}
	}
	l := write(t, e, a, []sessiongen.Turn{first, {Say: 30, Tools: 1, Output: 50}})
	r := e.Rig(t, a.name)
	run(t, r, a, l.Hooks)
	st := settled(r, lastPrompt(a, l.Hooks))
	exp := expected(a, l.Hooks)
	if exp[0].end != "interrupted" {
		t.Fatalf("the script's first turn ends %q", exp[0].end)
	}
	verify(t, "interrupt", a, st, exp, 0)
	checkHook(t, "interrupt", a, st, l, true)
	if st.Mission.Summary.Agents != 0 || st.Mission.Summary.Working {
		t.Errorf("summary after the interrupt: %+v", st.Mission.Summary)
	}
}

// orderScenario delivers the same session with its hooks duplicated,
// swapped and with one missing, as separate hook processes can. What the
// engine shows afterwards must be sound: no id twice, nothing running,
// the summary idle. It returns the engine's state for each variant.
func orderScenario(t *testing.T, a hookAgent, mutate func(calls []sessiongen.HookCall) []sessiongen.HookCall) (enginetest.State, *sessiongen.Layout) {
	e := support.NewEnv(t, time.Hour)
	l := write(t, e, a, mixed(a, 8))
	calls := mutate(append([]sessiongen.HookCall(nil), l.Hooks...))
	r := e.Rig(t, a.name)
	run(t, r, a, calls)
	st := r.SettleUntil(20*time.Second, nil)
	return st, l
}

// concurrent reads the session from many goroutines while hooks arrive.
func concurrent(t *testing.T, a hookAgent) {
	e := support.NewEnv(t, time.Hour)
	l := write(t, e, a, mixed(a, 40))
	r := e.Rig(t, a.name)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ctx := context.Background()
				sub := &quiet{done: make(chan struct{})}
				res, err := r.Engine.Open(ctx, sub, r.ID, 0, 20+g)
				if err != nil {
					t.Errorf("open: %v", err)
					return
				}
				if len(res.Items) > 0 {
					if _, err := r.Engine.History(ctx, r.ID, res.Items[0].Seq, 10); err != nil {
						t.Errorf("history: %v", err)
						return
					}
				}
				_, _ = r.Engine.Describe(ctx, r.ID)
				r.Engine.Summary(r.ID)
				_ = r.Engine.Close(ctx, sub, r.ID)
				close(sub.done)
			}
		}(g)
	}
	run(t, r, a, l.Hooks)
	close(stop)
	wg.Wait()
	st := settled(r, lastPrompt(a, l.Hooks))
	exp := expected(a, l.Hooks)
	verify(t, "concurrent", a, st, exp, 0)
	checkHook(t, "concurrent", a, st, l, true)
}

type quiet struct{ done chan struct{} }

func (q *quiet) Notify(harness.Notification) error { return nil }
func (q *quiet) Done() <-chan struct{}             { return q.done }

// connectionDrop: the client's connection drops while the agent works and
// comes back with the cursor it had. It gets exactly the items that
// arrived meanwhile, each once, and the same epoch.
func connectionDrop(t *testing.T, a hookAgent) {
	e := support.NewEnv(t, time.Hour)
	l := write(t, e, a, mixed(a, 9))
	r := e.Rig(t, a.name)
	half := len(l.Hooks) / 2
	run(t, r, a, l.Hooks[:half])
	drain(r)
	ctx := context.Background()
	sub := &quiet{done: make(chan struct{})}
	first, err := r.Engine.Open(ctx, sub, r.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Engine.Close(ctx, sub, r.ID)
	close(sub.done) // the connection drops
	run(t, r, a, l.Hooks[half:])
	st := settled(r, lastPrompt(a, l.Hooks))
	sub2 := &quiet{done: make(chan struct{})}
	back, err := r.Engine.Open(ctx, sub2, r.ID, first.Cursor, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer close(sub2.done)
	if back.Epoch != first.Epoch {
		t.Errorf("the epoch changed from %q to %q while the client was away", first.Epoch, back.Epoch)
	}
	want := map[string]int64{}
	for _, it := range st.Items {
		if it.Seq > first.Cursor {
			want[it.ID] = it.Seq
		}
	}
	got := map[string]int64{}
	for _, it := range back.Items {
		if _, dup := got[it.ID]; dup {
			t.Errorf("%s comes twice after the reconnect", it.ID)
		}
		got[it.ID] = it.Seq
	}
	if len(got) != len(want) {
		t.Errorf("the reconnect returned %d items, %d arrived meanwhile", len(got), len(want))
	}
	for id, seq := range want {
		if got[id] != seq {
			t.Errorf("%s: seq %d after the reconnect, %d in the log", id, got[id], seq)
		}
	}
	if back.Cursor != st.Items[len(st.Items)-1].Seq {
		t.Errorf("cursor %d, newest item %d", back.Cursor, st.Items[len(st.Items)-1].Seq)
	}
}

// sessionRemoved: the session is removed (the hub reports it gone) while
// the agent has subagents running; nothing of it stays, and the same id
// served again starts clean.
func sessionRemoved(t *testing.T, a hookAgent) {
	e := support.NewEnv(t, time.Hour)
	turns := mixed(a, 6)
	if a.canAgents {
		turns[5].Agents = &sessiongen.Batch{Size: 3, Background: true}
	}
	l := write(t, e, a, turns)
	r := e.Rig(t, a.name)
	run(t, r, a, l.Hooks[:len(l.Hooks)-1])
	drain(r)
	if len(r.Read().Items) == 0 {
		t.Fatal("nothing was drawn before the removal")
	}
	r.Engine.Forget(r.ID)
	st := r.Read()
	if len(st.Items) != 0 || st.Mission.Summary.Agents != 0 || st.Mission.Summary.Working {
		t.Errorf("after the removal: %d items, summary %+v", len(st.Items), st.Mission.Summary)
	}
	for _, sec := range st.Mission.Sections {
		if len(sec.Rows) != 0 {
			t.Errorf("section %s still has %d rows", sec.Kind, len(sec.Rows))
		}
	}
}

// atLimit: a log that holds 60 items and tool output of 70 KB. The oldest
// items go, the newest turns stay whole, and no text is longer than the
// limits.
func atLimit(t *testing.T, a hookAgent) {
	e := support.NewEnv(t, time.Hour)
	turns := mixed(a, 24)
	for i := range turns {
		if i%4 == 1 {
			turns[i].Hook.Large = 70 << 10
		}
	}
	l := write(t, e, a, turns)
	r := enginetest.New(t, enginetest.Options{Agent: a.name, Home: e.Home, Cwd: e.Cwd, Created: e.Start.Add(-30 * time.Second), MaxItems: 60, ID: enginetest.NewID(), NoRedact: true})
	run(t, r, a, l.Hooks)
	st := settled(r, lastPrompt(a, l.Hooks))
	if st.Discarded == 0 || len(st.Items) > 60 {
		t.Errorf("%d items kept, discarded through %d", len(st.Items), st.Discarded)
	}
	exp := expected(a, l.Hooks)
	verify(t, "at the limit", a, st, exp, len(exp)-3)
	checkHook(t, "at the limit", a, st, l, true)
	for _, it := range st.Items {
		if it.Tool != nil && len(it.Tool.Output) > harness.MaxToolOutput+16 {
			t.Errorf("%s: %d bytes of output, limit %d", it.ID, len(it.Tool.Output), harness.MaxToolOutput)
		}
		if it.Assistant != nil && len(it.Assistant.Text) > harness.MaxItemText {
			t.Errorf("%s: %d bytes of text, limit %d", it.ID, len(it.Assistant.Text), harness.MaxItemText)
		}
	}
	enginetest.Fail(t, "paging", enginetest.SamePaging(st, r.ReadPages(7)))
}

// questions: the agent's own question tool. One is answered, one is
// cancelled, one is open when the next prompt comes. While it is open the
// summary says the user is needed, and every one is closed after.
func questions(t *testing.T, a hookAgent, plan bool) {
	e := support.NewEnv(t, time.Hour)
	ask := func(answer string, cancel bool) *sessiongen.HookAsk {
		h := &sessiongen.HookAsk{Prompt: "Which one?", Options: []string{"Red", "Green"}, Answer: answer, Cancel: cancel}
		if plan {
			h.Plan = "Add the header and test it."
		}
		return h
	}
	turns := []sessiongen.Turn{
		{Say: 20, Tools: 1, Hook: &sessiongen.HookTurn{Ask: ask("Green", false)}},
		{Say: 20, Tools: 1, Hook: &sessiongen.HookTurn{Ask: ask("", true)}},
		{Say: 20, Tools: 1, Hook: &sessiongen.HookTurn{Ask: ask("", false), Pending: true}},
		{Say: 20, Tools: 1},
	}
	l := write(t, e, a, turns)
	r := e.Rig(t, a.name)
	// Up to the third prompt's question: open.
	var prompts []int
	for i, c := range l.Hooks {
		for _, k := range a.classify(c) {
			if k.what == "prompt" {
				prompts = append(prompts, i)
			}
		}
	}
	run(t, r, a, l.Hooks[:prompts[3]])
	open := r.SettleUntil(10*time.Second, func(st enginetest.State) bool { return st.Mission.Summary.NeedsYou == 1 })
	var q *harness.QuestionBody
	for _, it := range open.Items {
		if it.Question != nil && len(it.Question.Answered) == 0 {
			q = it.Question
		}
	}
	if q == nil || len(q.Questions) != 1 || len(q.Questions[0].Options) == 0 {
		t.Fatalf("no open question with its options while the agent waits: %+v", q)
	}
	needs := 0
	for _, sec := range open.Mission.Sections {
		if sec.Kind == harness.SectionNeeds {
			needs = len(sec.Rows)
		}
	}
	if needs != 1 {
		t.Errorf("%d rows in Needs while a question is open", needs)
	}
	run(t, r, a, l.Hooks[prompts[3]:])
	st := settled(r, lastPrompt(a, l.Hooks))
	answers := map[string][]string{}
	var qs []harness.Item
	for _, it := range st.Items {
		if it.Question != nil {
			qs = append(qs, it)
		}
	}
	if len(qs) != 3 {
		t.Fatalf("%d questions in the chat, want 3", len(qs))
	}
	for i, it := range qs {
		if len(it.Question.Answered) == 0 {
			t.Errorf("question %d is still open", i)
		}
		for id, v := range it.Question.Answered {
			answers[it.ID+id] = v
		}
	}
	if got := qs[0].Question.Answered[qs[0].Question.Questions[0].ID]; len(got) != 1 || got[0] != "Green" {
		if !plan { // a plan's answer is the proposal taken
			t.Errorf("the answered question holds %v, want [Green]", got)
		}
	}
	// A cancelled question holds no answer; a refused plan holds the
	// refusal the agent took.
	got := qs[1].Question.Answered[qs[1].Question.Questions[0].ID]
	if plan && len(got) != 1 || !plan && len(got) != 0 {
		t.Errorf("the cancelled question holds %v", got)
	}
	if st.Mission.Summary.NeedsYou != 0 {
		t.Errorf("%d things need you after every question closed", st.Mission.Summary.NeedsYou)
	}
	checkHook(t, "questions", a, st, l, true)
}

// agentRows: subagents the agent starts show as running rows (one each,
// even when they share a type) until a stop hook or the end of the turn
// closes them.
func agentRows(t *testing.T, a hookAgent) {
	e := support.NewEnv(t, time.Hour)
	turns := []sessiongen.Turn{
		{Say: 20, Tools: 1, Agents: &sessiongen.Batch{Size: 3, Background: true}},
		{Say: 20, Tools: 1, Agents: &sessiongen.Batch{Size: 2}},
	}
	l := write(t, e, a, turns)
	r := e.Rig(t, a.name)
	var ends []int
	for i, c := range l.Hooks {
		if c.End != "" {
			ends = append(ends, i)
		}
	}
	rows := func(st enginetest.State) (running, all int) {
		for _, sec := range st.Mission.Sections {
			if sec.Kind != harness.SectionAgents {
				continue
			}
			for _, row := range sec.Rows {
				all++
				if runningState(row.State) {
					running++
				}
			}
		}
		return
	}
	// Three running before the first turn ends.
	run(t, r, a, l.Hooks[:ends[0]])
	st := r.SettleUntil(10*time.Second, func(st enginetest.State) bool { return st.Mission.Summary.Agents == 3 })
	if n, all := rows(st); n != 3 || all != 3 {
		t.Errorf("%d running rows of %d, want 3 of 3", n, all)
	}
	// None after the turn ends.
	run(t, r, a, l.Hooks[ends[0]:ends[0]+1])
	st = r.SettleUntil(10*time.Second, func(st enginetest.State) bool { return st.Mission.Summary.Agents == 0 })
	if n, _ := rows(st); n != 0 {
		t.Errorf("%d rows still run after the turn ended", n)
	}
	// A foreground batch stops before the turn ends.
	run(t, r, a, l.Hooks[ends[0]+1:ends[1]])
	st = r.SettleUntil(10*time.Second, func(st enginetest.State) bool { return st.Mission.Summary.Working })
	if n, _ := rows(st); n != 0 || st.Mission.Summary.Agents != 0 {
		t.Errorf("%d rows run after the foreground batch stopped (summary %d)", n, st.Mission.Summary.Agents)
	}
	run(t, r, a, l.Hooks[ends[1]:])
	st = settled(r, lastPrompt(a, l.Hooks))
	checkHook(t, "agents", a, st, l, true)
}

func runningState(s string) bool { return s == harness.StateRunning || s == harness.StateWaiting }
