// Package local serves the methods that host-side helpers call on the
// daemon's unix socket: agent hooks (local.hook), and the notify, ask and
// job commands scripts use. None of them is listed in hello capabilities.
package local

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Hook event names shared by the agent adapters. Adapters reduce their
// agent's own event names to these.
const (
	EventPrompt     = "prompt"     // the user sent a prompt: working
	EventTool       = "tool"       // a tool is about to run: working, activity = tool
	EventToolDone   = "tool_done"  // a tool finished: any prompt for it was answered at the desk
	EventPermission = "permission" // the agent asks for permission: open an approval
	EventAttention  = "attention"  // the agent waits for the user without a permission prompt
	// EventIdle is the agent sitting at its input with nothing asked (Claude
	// Code's idle_prompt). It is not a request: it never rings as needs_you.
	EventIdle    = "idle"
	EventNotify  = "notify"  // an informational agent notification
	EventStop    = "stop"    // the turn ended: idle and done
	EventStart   = "start"   // a session started; carries the model when known
	EventMeta    = "meta"    // model, context and cost from the status line
	EventEnd     = "end"     // the agent is quitting: ended
	EventFailure = "failure" // the turn stopped on an error: failed
	// EventUsageLimit is EventFailure on a rate or usage limit. The hook
	// params have no field for the reason, so it travels as its own name.
	EventUsageLimit = "usage_limit"
	// EventHarness only carries a raw payload to the harness adapter; it
	// changes no session state.
	EventHarness = "harness"
)

const (
	statusWorking = "working"
	statusIdle    = "idle"
	maxJobName    = wire.MaxTitle
)

// Concurrent lists the local methods that block and must run off the
// connection's read loop (rpc.HelperInfo.Concurrent).
var Concurrent = []string{wire.MethodLocalAsk}

// Hub is the part of the event hub the hook handler drives.
type Hub interface {
	SetStatus(id, status string, activity *string)
	SetMeta(id string, m wire.SessionMeta)
	SetExit(id string, status string, exit *wire.SessionExit)
	ClearExit(id string)
	HookExitAllowed(id string) bool
}

// Approvals is the part of the approvals store the handlers use.
type Approvals interface {
	Pending(sessionID string) []wire.Approval
	OpenFromHook(ctx context.Context, sessionID string, ev wire.HookEvent) wire.Approval
	OpenAsk(ctx context.Context, p wire.AskParams) (wire.Approval, <-chan string)
	CloseDesk(requestID string)
	Expire(requestID string)
}

// Handlers serves local.hook, local.notify, local.ask and local.job.
type Handlers struct {
	Hub       Hub
	Approvals Approvals
	Pub       wire.Publisher
	// Reach reports whether a notification for the session reaches the user
	// (a foreground subscriber or a paired device). Nil means always.
	Reach func(sessionID *string) bool
	// Clock and NewJobID default to time.Now and wire.NewJobID.
	Clock    func() time.Time
	NewJobID func() string
	// Harness, when set, receives every hook of a managed session with the
	// agent's raw payload (nil from helpers that do not forward it), after
	// the hook's own effects.
	Harness func(sessionID string, ev wire.HookForward)
	// Agents, when set, names each session's agent, so a hook that fired
	// inside a session started for another agent is dropped (Foreign).
	Agents *AgentCache

	mu   sync.Mutex
	meta map[string]wire.SessionMeta
	jobs map[string]wire.Job
	// turns marks sessions with a turn under way that has not yet been
	// reported done (by Stop, or by an idle prompt standing in for it).
	turns map[string]bool
}

// Methods returns the four local methods.
func (h *Handlers) Methods() map[string]api.Method {
	return map[string]api.Method{
		wire.MethodLocalHook:   h.hook,
		wire.MethodLocalNotify: h.notify,
		wire.MethodLocalAsk:    h.ask,
		wire.MethodLocalJob:    h.job,
	}
}

func invalid(msg string) error { return &session.InvalidParamsError{Msg: msg} }

func decode(raw json.RawMessage, v any) error {
	if err := api.Decode(raw, v); err != nil {
		return invalid("invalid params")
	}
	return nil
}

func (h *Handlers) now() time.Time {
	if h.Clock != nil {
		return wire.Stamp(h.Clock())
	}
	return wire.Stamp(time.Now())
}

// managedID validates an optional session id. Hooks may only speak for
// managed sessions; an absent id is allowed and returned as "".
func managedID(id *string) (string, error) {
	if id == nil {
		return "", nil
	}
	managed, err := session.ValidateID(*id)
	if err != nil {
		return "", err
	}
	if !managed {
		return "", invalid("only managed sessions accept local events")
	}
	return *id, nil
}

func (h *Handlers) hook(ctx context.Context, raw json.RawMessage) (any, error) {
	var fwd wire.HookForward
	if err := decode(raw, &fwd); err != nil {
		return nil, err
	}
	ev := fwd.HookEvent
	if !HookAgents[ev.Agent] {
		return nil, invalid("unknown agent")
	}
	id, err := managedID(ev.SessionID)
	if err != nil {
		return nil, err
	}
	switch ev.Event {
	case EventPrompt, EventTool, EventToolDone, EventPermission, EventAttention, EventIdle,
		EventNotify, EventStop, EventStart, EventMeta, EventEnd, EventFailure, EventUsageLimit, EventHarness:
	default:
		return nil, invalid("unknown hook event")
	}
	if id == "" {
		// Nothing to attribute the event to; the hook is a no-op.
		return struct{}{}, nil
	}
	if h.Agents != nil && Foreign(h.Agents.Agent(ctx, id), ev.Agent) {
		// Another harness ran this agent's hook file; its events belong to
		// that harness's own hooks, so this copy is dropped.
		return struct{}{}, nil
	}
	if len(fwd.Payload) > wire.MaxHookPayload || (len(fwd.Payload) > 0 && !json.Valid(fwd.Payload)) || len(fwd.Hook) > 64 {
		fwd.Payload, fwd.Hook = nil, ""
	}
	if h.Harness != nil {
		defer h.Harness(id, fwd)
	}
	if ev.Event == EventHarness {
		return struct{}{}, nil
	}
	// The exit detail is cleaned from the text as sent: clip drops the
	// escape character but would leave the rest of a sequence behind.
	rawMessage := ev.Data.Message
	ev.Data.Message = clip(ev.Data.Message, wire.MaxBody)
	if clearsExit(ev.Event) {
		h.Hub.ClearExit(id)
	}
	switch ev.Event {
	case EventPrompt, EventTool, EventToolDone, EventPermission:
		h.setTurn(id, true)
	}
	switch ev.Event {
	case EventPrompt:
		// A new turn makes any prompt left from the last one moot.
		h.closeAgentPrompts(id)
		h.Hub.SetStatus(id, statusWorking, nil)
	case EventTool:
		var act *string
		if t := clip(ev.Data.Tool, 64); t != "" {
			act = &t
		}
		h.Hub.SetStatus(id, statusWorking, act)
	case EventToolDone:
		h.closeAgentPrompts(id)
		h.Hub.SetStatus(id, statusWorking, nil)
	case EventPermission:
		h.Approvals.OpenFromHook(ctx, id, ev)
	case EventAttention:
		h.publishNotify(id, wire.LevelNeedsYou, clip(firstNonEmpty(ev.Data.Message, "Needs you"), wire.MaxTitle), "")
	case EventNotify:
		h.publishNotify(id, wire.LevelInfo, clip(firstNonEmpty(ev.Data.Message, "Notification"), wire.MaxTitle), "")
	case EventStop:
		h.closeAgentPrompts(id)
		h.setTurn(id, false)
		h.Hub.SetStatus(id, statusIdle, nil)
		h.publishNotify(id, wire.LevelDone, agentTitle(ev.Agent)+" finished", ev.Data.Message)
	case EventIdle:
		// Idle at its input, the agent is asking nothing: a pending prompt
		// was answered, withdrawn or interrupted. A turn whose Stop never
		// came is reported done once; otherwise this says nothing.
		h.closeAgentPrompts(id)
		h.Hub.SetStatus(id, statusIdle, nil)
		if h.setTurn(id, false) {
			h.publishNotify(id, wire.LevelDone, agentTitle(ev.Agent)+" finished", "")
		}
	case EventStart, EventMeta:
		h.mergeMeta(id, ev.Data)
	case EventEnd, EventFailure, EventUsageLimit:
		h.hookExit(id, ev.Event, rawMessage)
	}
	return struct{}{}, nil
}

// hookExit records an exit an agent hook reported. One that arrives after
// a relaunch, before the new run's first hook, comes from the run the
// relaunch killed and is dropped.
func (h *Handlers) hookExit(id, event, rawMessage string) {
	if !h.Hub.HookExitAllowed(id) {
		return
	}
	h.closeAgentPrompts(id)
	h.setTurn(id, false)
	switch event {
	case EventEnd:
		// The wrapper reports the real code once the process is gone.
		code := 0
		h.Hub.SetExit(id, wire.StatusEnded, &wire.SessionExit{Code: &code, Reason: wire.ExitReasonExit})
	default:
		reason := wire.ExitReasonError
		if event == EventUsageLimit {
			reason = wire.ExitReasonUsageLimit
		}
		h.Hub.SetExit(id, wire.StatusFailed, &wire.SessionExit{Reason: reason, Detail: ExitDetail(rawMessage)})
	}
}

// clearsExit reports whether a hook event shows the agent running again
// after it ended or failed. The status line, notifications and the exit
// events themselves do not: Claude keeps drawing its status line and
// reports an idle prompt while it sits stopped on a usage limit.
func clearsExit(event string) bool {
	switch event {
	case EventMeta, EventNotify, EventAttention, EventIdle, EventEnd, EventFailure, EventUsageLimit:
		return false
	}
	return true
}

// ExitDetail cleans agent text into a SessionExit detail: cleaned like a
// push body, its first line only, at most wire.MaxExitDetail runes, and
// nil when nothing is left.
func ExitDetail(s string) *string {
	line, _, _ := strings.Cut(detect.CleanLine(s), "\n")
	line = strings.TrimSpace(line)
	if r := []rune(line); len(r) > wire.MaxExitDetail {
		line = strings.TrimSpace(string(r[:wire.MaxExitDetail]))
	}
	if line == "" || (wire.SessionExit{Reason: wire.ExitReasonError, Detail: &line}).Validate() != nil {
		return nil
	}
	return &line
}

// mergeMeta keeps what earlier hooks reported: SessionStart knows only the
// model, the status line knows all three.
func (h *Handlers) mergeMeta(id string, d wire.HookData) {
	h.mu.Lock()
	if h.meta == nil {
		h.meta = map[string]wire.SessionMeta{}
	}
	m := h.meta[id]
	if d.Model != "" {
		model := clip(d.Model, 80)
		m.Model = &model
	}
	if d.ContextPercent != nil && *d.ContextPercent >= 0 && *d.ContextPercent <= 100 {
		v := *d.ContextPercent
		m.ContextPercent = &v
	}
	if d.CostUSD != nil && *d.CostUSD >= 0 {
		v := *d.CostUSD
		m.CostUSD = &v
	}
	h.meta[id] = m
	h.mu.Unlock()
	h.Hub.SetMeta(id, m)
}

func (h *Handlers) publishNotify(id, level, title, body string) {
	var sid *string
	if id != "" {
		sid = &id
	}
	h.Pub.Publish(wire.EventNotify, sid, wire.NotifyEvent{Title: title, Body: clip(body, wire.MaxBody), Level: level})
}

func (h *Handlers) notify(_ context.Context, raw json.RawMessage) (any, error) {
	var p wire.NotifyParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	id, err := managedID(p.SessionID)
	if err != nil {
		return nil, err
	}
	title := clip(strings.TrimSpace(p.Title), wire.MaxTitle)
	if title == "" {
		return nil, invalid("title is required")
	}
	h.publishNotify(id, wire.LevelInfo, title, p.Body)
	delivered := true
	if h.Reach != nil {
		delivered = h.Reach(p.SessionID)
	}
	return wire.NotifyResult{Delivered: delivered}, nil
}

func (h *Handlers) ask(ctx context.Context, raw json.RawMessage) (any, error) {
	var p wire.AskParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if _, err := managedID(p.SessionID); err != nil {
		return nil, err
	}
	p.Title = strings.TrimSpace(p.Title)
	switch {
	case p.Title == "":
		return nil, invalid("title is required")
	case len(p.Options) > wire.MaxAskOptions:
		return nil, invalid("at most 9 options")
	case p.TimeoutSec < 1 || p.TimeoutSec > 3600:
		return nil, invalid("timeoutSec must be 1-3600")
	}
	for _, o := range p.Options {
		if strings.TrimSpace(o) == "" {
			return nil, invalid("options must not be empty")
		}
	}
	names := func(label string) bool {
		return slices.ContainsFunc(p.Options, func(o string) bool { return strings.EqualFold(strings.TrimSpace(o), strings.TrimSpace(label)) })
	}
	switch {
	case p.Approve != "" && !names(p.Approve):
		return nil, invalid("approve must name one of the options")
	case p.Deny != "" && !names(p.Deny):
		return nil, invalid("deny must name one of the options")
	case p.Approve != "" && strings.EqualFold(strings.TrimSpace(p.Approve), strings.TrimSpace(p.Deny)):
		return nil, invalid("approve and deny must name different options")
	}
	// Only the app answers an ask. With no app in the foreground and no
	// paired device nobody will see it, so waiting would only hold up the
	// caller (an agent's own permission dialog, for a blocking hook).
	if h.Reach != nil && !h.Reach(p.SessionID) {
		return wire.AskResult{Reason: wire.AskTimeout}, nil
	}
	a, ch := h.Approvals.OpenAsk(ctx, p)
	timer := time.NewTimer(time.Duration(p.TimeoutSec) * time.Second)
	defer timer.Stop()
	select {
	case d, ok := <-ch:
		if !ok {
			return wire.AskResult{Reason: wire.AskTimeout}, nil
		}
		return wire.AskResult{Decision: &d, Reason: wire.AskAnswered}, nil
	case <-timer.C:
		h.Approvals.Expire(a.RequestID)
		return wire.AskResult{Reason: wire.AskTimeout}, nil
	case <-ctx.Done():
		// The asking process went away; nobody is left to act on an answer.
		h.Approvals.Expire(a.RequestID)
		return nil, ctx.Err()
	}
}

func (h *Handlers) job(_ context.Context, raw json.RawMessage) (any, error) {
	var p wire.JobParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Progress != nil && (*p.Progress < 0 || *p.Progress > 100) {
		return nil, invalid("progress must be 0-100")
	}
	if p.Message != nil {
		m := clip(*p.Message, wire.MaxBody)
		p.Message = &m
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.jobs == nil {
		h.jobs = map[string]wire.Job{}
	}
	var j wire.Job
	switch p.Op {
	case wire.JobStart:
		name := clip(strings.TrimSpace(p.Name), maxJobName)
		if name == "" {
			return nil, invalid("name is required")
		}
		newID := h.NewJobID
		if newID == nil {
			newID = wire.NewJobID
		}
		j = wire.Job{JobID: newID(), Name: name, Progress: p.Progress, Message: p.Message, Status: wire.JobRunning, StartedAt: h.now()}
		h.jobs[j.JobID] = j
	case wire.JobUpdate, wire.JobEnd:
		var ok bool
		if j, ok = h.jobs[p.JobID]; !ok {
			return nil, &api.CodeError{Code: rpc.CodeNotFound}
		}
		if p.Progress != nil {
			j.Progress = p.Progress
		}
		if p.Message != nil {
			j.Message = p.Message
		}
		if p.Op == wire.JobEnd {
			if p.Status != wire.JobOK && p.Status != wire.JobFailed {
				return nil, invalid("status must be ok or failed")
			}
			end := h.now()
			j.Status, j.EndedAt = p.Status, &end
			delete(h.jobs, j.JobID)
		} else {
			h.jobs[j.JobID] = j
		}
	default:
		return nil, invalid("op must be start, update or end")
	}
	// Published under h.mu so a job's events keep their order.
	h.Pub.Publish(wire.EventJob, nil, wire.JobEvent{Job: j})
	return wire.JobResult{JobID: j.JobID}, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// clip trims s to n runes and drops control characters, so agent text can
// not carry terminal escapes into a notification.
func clip(s string, n int) string {
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count == n {
			break
		}
		if r == utf8.RuneError || r < 0x20 && r != '\n' || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			continue
		}
		b.WriteRune(r)
		count++
	}
	return strings.TrimSpace(b.String())
}
