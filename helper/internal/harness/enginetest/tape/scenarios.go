package tape

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Scenario is one tape the catalogue can make: a stable id, which agents
// it applies to, and a function that drives a rig.
type Scenario struct {
	ID      string
	Summary string
	// Applies returns "" when the scenario applies to the agent, or the
	// reason it does not.
	Applies func(agent string) string
	// Run drives the scenario and returns the world holding the tape. A
	// Skip error means the agent's generator cannot show the feature.
	Run func(t testing.TB, agent string) (*World, error)
}

// Catalogue lists the scenarios, in a fixed order.
func Catalogue() []Scenario {
	return []Scenario{
		{"long-chat-restart", "a long session, read to the foot, a helper restart, reopen",
			capability("chat"), longChatRestart},
		{"approval-answered", "an approval arrives and is answered from the phone",
			approvalApplies, approvalAnswered},
		{"question-answered", "an agent question is open, then answered",
			capability("needs.questions"), questionAnswered},
		{"send-while-working", "a prompt queued while the agent works is delivered when it stops",
			capability("send.queue"), sendWhileWorking},
		{"interrupt", "the phone interrupts a working turn",
			capability("interrupt"), interrupted},
		{"subagents", "a batch of background subagents starts and finishes",
			capability("agents"), subagents},
		{"new-conversation", "the agent moves to a new conversation and the chat starts over",
			lifecycle("new_conversation"), newConversation},
	}
}

// Lookup finds a scenario by id.
func Lookup(id string) (Scenario, bool) {
	for _, s := range Catalogue() {
		if s.ID == id {
			return s, true
		}
	}
	return Scenario{}, false
}

var (
	cellsOnce sync.Once
	cells     map[string]coverage.Cell
	cellsErr  error
)

func inventory() (map[string]coverage.Cell, error) {
	cellsOnce.Do(func() {
		list, err := coverage.Cells()
		if err != nil {
			cellsErr = err
			return
		}
		cells = coverage.Index(list)
	})
	return cells, cellsErr
}

// capability applies to the agents whose adapter declares the capability.
func capability(name string) func(string) string {
	return func(agent string) string {
		idx, err := inventory()
		if err != nil {
			return "no inventory: " + err.Error()
		}
		if _, ok := idx[Base(agent)+"/"+coverage.KindCapability+"/"+name]; !ok {
			return agent + "'s adapter declares no " + name + " capability"
		}
		return ""
	}
}

// lifecycle applies to the agents the inventory asks about a lifecycle event.
func lifecycle(name string) func(string) string {
	return func(agent string) string {
		idx, err := inventory()
		if err != nil {
			return "no inventory: " + err.Error()
		}
		c, ok := idx[Base(agent)+"/"+coverage.KindLifecycle+"/"+name]
		switch {
		case !ok:
			return agent + " is not in the coverage inventory"
		case c.NotApplicable != "":
			return c.NotApplicable
		}
		return ""
	}
}

func approvalApplies(agent string) string {
	if why := capability("needs.approvals")(agent); why != "" {
		return why
	}
	if dialogFor(agent) == "" {
		return "no captured permission screen for " + agent + " in detect/testdata: its approvals do not come from the screen"
	}
	return ""
}

// turns returns n copies of a turn.
func turns(n int, t sessiongen.Turn) []sessiongen.Turn {
	out := make([]sessiongen.Turn, n)
	for i := range out {
		out[i] = t
	}
	return out
}

var shortChat = []sessiongen.Turn{{Say: 80, Tools: 1, Output: 120}, {Say: 60}}

// IsStreamed reports an agent whose chat comes from a server's event
// stream (OpenCode and Kilo).
func IsStreamed(agent string) bool { return enginetest.IsAPIAgent(Base(agent)) }

// variants are profiles of an agent's other generation: the inventory,
// the captures and the API server know the agent by its base name.
var variants = map[string]string{"opencode2": "opencode"}

// Base is the agent a profile belongs to: opencode for opencode2.
func Base(agent string) string {
	if b, ok := variants[agent]; ok {
		return b
	}
	return agent
}

// hasReply is true when the page holds an assistant reply. Droid's chat
// has none for any prompt, so a scenario does not require one; the pilot's
// test does.
func hasReply(items []harness.Item) bool {
	for _, it := range items {
		if it.Assistant != nil && it.Assistant.Text != "" {
			return true
		}
	}
	return false
}

// longChatRestart: 35 turns, the app opens the session, the helper
// restarts, and the app reopens: with its cursor, then from the start.
func longChatRestart(t testing.TB, agent string) (*World, error) {
	t.Helper()
	all := turns(35, sessiongen.Turn{Say: 320, Tools: 2, Output: 200})
	first := all
	if IsStreamed(agent) {
		// The agent's server brings the session as a stream: it is
		// written in pieces while the helper reads it, as it grows.
		first = all[:4]
	}
	w, err := NewWorld(t, "long-chat-restart", agent, slices.Clone(first))
	if err != nil {
		return nil, err
	}
	w.Start()
	for done := len(first); done < len(all); {
		next := min(done+5, len(all))
		if err := w.Append(slices.Clone(all[done:next])...); err != nil {
			return nil, err
		}
		done = next
		w.SettleTurns(time.Minute, done)
	}
	w.Settle(3 * time.Minute)
	rec := w.Rec
	w.Quiet()
	rec.Describe()
	initial := rec.Open(0)
	if len(initial.Items) == 0 {
		return nil, fmt.Errorf("the first page is empty")
	}
	rec.Restart()
	if IsStreamed(agent) {
		w.Rig.Settle(time.Minute)
	} else {
		w.Settle(90 * time.Second)
	}
	w.Quiet()
	resumed, fresh := rec.Reopen()
	if len(fresh.Items) == 0 && IsStreamed(agent) {
		return nil, Skip("after a helper restart the engine shows no chat for " + agent + ": its event stream carries no history (e2e apiRestartLosesChat)")
	}
	switch {
	case initial.Epoch == resumed.Epoch:
		return nil, fmt.Errorf("the restart did not give the session a new epoch (%s)", resumed.Epoch)
	case resumed.Epoch != fresh.Epoch:
		return nil, fmt.Errorf("the reopen results name two epochs: %s, %s", resumed.Epoch, fresh.Epoch)
	case len(fresh.Items) == 0:
		return nil, fmt.Errorf("the final page is empty: the restart lost the chat")
	}
	rec.Flush()
	return w, nil
}

// live starts a world whose last turn is at work, and opens it in the app.
func live(t testing.TB, scenario, agent string) (*World, error) {
	t.Helper()
	w, err := NewWorld(t, scenario, agent, slices.Clone(shortChat))
	if err != nil {
		return nil, err
	}
	if err := w.Hold(); err != nil {
		return nil, err
	}
	w.Start()
	w.Working()
	w.Quiet()
	w.Rec.Describe()
	w.Rec.Open(0)
	return w, nil
}

// approvalAnswered: the agent's permission prompt reaches the phone and
// the phone approves it.
func approvalAnswered(t testing.TB, agent string) (*World, error) {
	t.Helper()
	w, err := live(t, "approval-answered", agent)
	if err != nil {
		return nil, err
	}
	r := w.Rig
	r.Term.Open(&enginetest.Dialog{Screen: r.Term.MustCapture(dialogFor(agent))})
	r.Look()
	p := r.WaitPending(1, 5*time.Second)[0]
	r.SettleUntil(10*time.Second, func(st enginetest.State) bool { return st.Mission.Summary.NeedsYou > 0 })
	w.Rec.Flush()
	if _, err := w.Rec.Call(wire.MethodApprovalAnswer, map[string]any{"requestId": p.RequestID, "decision": wire.DecisionApprove}); err != nil {
		return nil, fmt.Errorf("approval.answer: %w", err)
	}
	if got := r.Closed()[p.RequestID]; got != wire.ByPhone {
		return nil, fmt.Errorf("the approval closed as %q, want %q", got, wire.ByPhone)
	}
	r.Look()
	w.Idle()
	w.Rec.Flush()
	return w, nil
}

// questionAnswered: a question the agent asked is open when the app
// opens the session; the agent's record then shows it answered.
func questionAnswered(t testing.TB, agent string) (*World, error) {
	t.Helper()
	ask := &sessiongen.HookAsk{Prompt: "Which colour should the banner use?", Options: []string{"red", "green", "blue"}, Answer: "green"}
	list := slices.Clone(shortChat)
	list = append(list, sessiongen.Turn{Say: 30, Hook: &sessiongen.HookTurn{Ask: ask, Pending: true}})
	w, err := NewWorld(t, "question-answered", agent, list)
	if err != nil {
		return nil, err
	}
	w.Start()
	st := w.Rig.SettleUntil(30*time.Second, nil)
	if !openQuestion(st) {
		return nil, Skip("the " + agent + " profile writes no open question the engine shows")
	}
	w.Quiet()
	w.Rec.Describe()
	w.Rec.Open(0)
	if err := w.Append(sessiongen.Turn{Prompt: sessiongen.Continue, Say: 30}); err != nil {
		return nil, err
	}
	st = w.Rig.SettleUntil(30*time.Second, func(st enginetest.State) bool { return !openQuestion(st) })
	if openQuestion(st) {
		return nil, fmt.Errorf("the question is still open after the agent went on")
	}
	w.Rec.Flush()
	return w, nil
}

func openQuestion(st enginetest.State) bool {
	latest := map[string]harness.Item{}
	for _, it := range st.Items {
		latest[it.ID] = it
	}
	for _, it := range latest {
		if it.Question != nil && len(it.Question.Answered) == 0 {
			return true
		}
	}
	return false
}

// sendWhileWorking: the phone queues a prompt behind the running turn;
// when the turn ends the engine types it.
func sendWhileWorking(t testing.TB, agent string) (*World, error) {
	t.Helper()
	w, err := live(t, "send-while-working", agent)
	if err != nil {
		return nil, err
	}
	const text = "Queued while the agent works"
	res, err := w.Rec.Send(text, harness.SendQueue)
	if err != nil {
		return nil, fmt.Errorf("queue: %w", err)
	}
	if res.Item.User == nil || !res.Item.User.Queued {
		return nil, fmt.Errorf("the prompt was not queued: %+v", res.Item)
	}
	w.Rig.SettleUntil(10*time.Second, func(st enginetest.State) bool { return len(st.Items) > 0 })
	w.Rec.Flush()
	w.Idle()
	if !wait(15*time.Second, func() bool { return slices.Contains(w.Rig.Term.Submitted(), text) }) {
		return nil, fmt.Errorf("the queued prompt was never typed")
	}
	w.Rig.SettleUntil(15*time.Second, func(st enginetest.State) bool {
		for _, it := range st.Items {
			if it.ID == res.Item.ID && it.User != nil && !it.User.Queued {
				return true
			}
		}
		return false
	})
	w.Rec.Flush()
	return w, nil
}

func wait(limit time.Duration, ok func() bool) bool {
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return true
		}
	}
	return ok()
}

// interrupted: the phone interrupts the working turn; the agent stops.
func interrupted(t testing.TB, agent string) (*World, error) {
	t.Helper()
	w, err := live(t, "interrupt", agent)
	if err != nil {
		return nil, err
	}
	r := w.Rig
	stopped := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { w.EndTurn(); r.SetHubStatus("idle"); close(stopped) }) }
	if w.hooks {
		// A hook call waits on the engine: end the turn beside it.
		r.Term.OnInterrupt = func(string) { go stop() }
	} else {
		// The engine checks the agent stopped within a short window: end
		// the turn before the press returns, as an agent that stops at once.
		r.Term.OnInterrupt = func(string) { stop() }
	}
	res, err := w.Rec.Interrupt()
	if err != nil {
		return nil, fmt.Errorf("interrupt: %w", err)
	}
	if res.Result == harness.ResultFailed || res.Result == harness.ResultFallback {
		return nil, fmt.Errorf("interrupt: %+v", res)
	}
	if c := r.Codex(); c == nil && r.API() == nil {
		if keys := r.Term.KeysSent(); !slices.Contains(keys, "Escape") && !slices.Contains(keys, "C-c") {
			return nil, fmt.Errorf("no interrupt key reached the agent: %v", keys)
		}
	} else {
		stop()
	}
	select {
	case <-stopped:
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("the agent never stopped")
	}
	r.SettleUntil(20*time.Second, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	w.Rec.Flush()
	return w, nil
}

// subagents: a turn starts a batch of background subagents, and they
// finish.
func subagents(t testing.TB, agent string) (*World, error) {
	t.Helper()
	w, err := NewWorld(t, "subagents", agent, slices.Clone(shortChat))
	if err != nil {
		return nil, err
	}
	w.Start()
	w.Settle(time.Minute)
	w.Quiet()
	w.Rec.Describe()
	w.Rec.Open(0)
	if err := w.Append(sessiongen.Turn{Say: 40, Agents: &sessiongen.Batch{Size: 3, Steps: 1, Background: true}}); err != nil {
		return nil, err
	}
	st := w.Rig.SettleUntil(30*time.Second, func(st enginetest.State) bool {
		n := 0
		for _, it := range st.Items {
			if it.Agent != nil {
				n++
			}
		}
		return n >= 3
	})
	if !slices.ContainsFunc(st.Items, func(it harness.Item) bool { return it.Agent != nil }) {
		return nil, Skip("the engine draws no subagent rows from the " + agent + " profile's session")
	}
	w.Rec.Flush()
	return w, nil
}

// newConversation: the agent moves to a new conversation; the engine
// resets the chat and the app opens it again.
func newConversation(t testing.TB, agent string) (*World, error) {
	t.Helper()
	w, err := NewWorld(t, "new-conversation", agent, slices.Clone(shortChat))
	if err != nil {
		return nil, err
	}
	w.Start()
	w.Settle(time.Minute)
	w.Quiet()
	w.Rec.Describe()
	first := w.Rec.Open(0)
	if err := w.Append(sessiongen.Turn{New: true, Say: 40, Tools: 1, Output: 60}); err != nil {
		return nil, err
	}
	// The feeds look for a new file every couple of seconds.
	if !wait(20*time.Second, func() bool { w.Rec.Flush(); return w.Rec.Notes[harness.NoteReset] > 0 }) {
		return nil, Skip("the engine sent no reset when the " + agent + " profile started a new conversation")
	}
	w.Rig.SettleUntil(15*time.Second, nil)
	w.Rec.Flush()
	again := w.Rec.Open(0)
	if again.Epoch == first.Epoch {
		return nil, Skip("the engine keeps the old chat when the " + agent + " profile starts a new conversation (epoch unchanged)")
	}
	return w, nil
}
