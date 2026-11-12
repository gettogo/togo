package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gettogo/togo/helper/internal/wire"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/procs"
	"github.com/gettogo/togo/helper/internal/harness/source"
	"github.com/gettogo/togo/helper/internal/session"
)

// sess is one served session.
type sess struct {
	e    *Engine
	id   string
	info session.Session
	ad   *adapter

	ctx    context.Context
	cancel context.CancelFunc
	ready  chan struct{}
	once   sync.Once

	mu sync.Mutex
	// starting serialises starting the harness's session for a setting
	// picked before the first prompt (startSession).
	starting   sync.Mutex
	env        harness.Env
	version    string
	planWarned bool // the unrecognised-todo-format line was logged
	gateOK     bool
	gateWhy    string
	degraded   map[string]bool
	mappers    map[string]harness.Mapper // by mapper name, shared by feeds
	feeds      map[string]*feedRun
	last       map[string]json.RawMessage // latest record per feed
	anchors    map[string]*harness.Anchor
	mission    *missionState
	subs       map[harness.Subscriber]*subQueue
	queue      []queued
	sent       map[string]bool // queued prompts already typed, by item id
	phone      []phoneText
	working    bool
	// stoppedAt is when a stop last ended the turn (see stopHold).
	stoppedAt time.Time
	// feedWorking is what the mappers last said about the turn, and
	// feedEndedAt when they last said it ended (see feedTrust).
	feedWorking bool
	feedEndedAt time.Time
	// awaitReply is set when a prompt from the phone was typed and
	// nothing the agent said has arrived since (see replyMissingLocked).
	awaitReply bool
	// awaitText is the prompt awaitReply waits on, never read as its error.
	awaitText string
	// exitNoticed is set once the chat was told the agent exited, until
	// it runs again.
	exitNoticed bool
	// failedTurn is the turn the screen ended as failed while its hooks
	// still said it ran (see hookfail.go); failedHold is set while it holds.
	failedTurn string
	failedHold bool
	// relaunchedAt is when an action last restarted the agent.
	relaunchedAt time.Time
	// statusModelFromState is the status block's model when the session's
	// state filled it in (no status line named one), so a later switch
	// replaces it.
	statusModelFromState string

	turn     string
	summaryT *time.Timer
	srcRows  map[string]map[string]bool // mission rows each section's source listed
	// sum mirrors the mission summary for readers that must not take mu
	// (the hub decorates sessions while holding its own lock).
	sum      atomic.Pointer[harness.Summary]
	pending  map[string]harness.Item // coalesced partial updates
	sentText map[string]string       // streaming item id -> the whole text subscribers hold
	lastPart map[string]time.Time
	needs    needsState
	seed     screenSeed
	// dialog is the title of the dialog the typing guard last reported.
	dialog string
	// settingUpUntil is the end of a sign-in started here: until then the
	// sources it changes are read again on each screen (see fetch).
	settingUpUntil time.Time
	// labels is what the model picker calls each model id (model_label.go).
	labels modelLabels
}

type queued struct {
	item harness.Item
}

// phoneText is a prompt typed from the phone, waiting for the agent's own
// record of it.
type phoneText struct {
	id     string
	text   string
	images []harness.ImageRef
	at     time.Time
}

type feedRun struct {
	name     string
	rendered string
	cancel   context.CancelFunc
	failures int
}

func newSess(e *Engine, info session.Session) *sess {
	ctx, cancel := context.WithCancel(context.Background())
	s := &sess{
		e: e, id: info.ID, info: info, ctx: ctx, cancel: cancel, ready: make(chan struct{}),
		degraded: map[string]bool{}, mappers: map[string]harness.Mapper{}, feeds: map[string]*feedRun{},
		last: map[string]json.RawMessage{}, anchors: map[string]*harness.Anchor{},
		subs: map[harness.Subscriber]*subQueue{}, pending: map[string]harness.Item{}, lastPart: map[string]time.Time{}, sentText: map[string]string{},
	}
	if info.Managed {
		s.ad = e.adapterFor(info.Agent, info.Command, e.envFor(info))
	}
	if s.ad == nil {
		close(s.ready)
		return s
	}
	s.env = e.envFor(info)
	s.mission = newMission(s.missionCaps())
	return s
}

// missionCaps is the adapter's capabilities plus the Needs section every
// served session has: the screen detector's approvals and the engine's
// question rows reach the chat whatever the adapter declares, as the
// descriptor says too.
func (s *sess) missionCaps() []string {
	caps := s.ad.Capabilities
	if s.hasNeeds() && !harness.Has(caps, harness.CapNeedsApprovals) && !harness.Has(caps, harness.CapNeedsQuestions) {
		caps = append(slices.Clone(caps), harness.CapNeedsApprovals)
	}
	return caps
}

// envFor builds the template and runner environment of a session.
func (e *Engine) envFor(info session.Session) harness.Env {
	home := e.cfg.Home
	cwd := source.Expand(info.Cwd, home)
	env := harness.Env{SessionID: info.ID, Agent: info.Agent, Cwd: cwd, Argv: info.Command, Vars: map[string]string{
		source.VarHome: home, "cwd": cwd, "cwd_key": CwdKey(cwd), "session": info.ID,
		"created": info.CreatedAt.UTC().Format(time.RFC3339),
		source.VarTogoHome: e.cfg.TogoHome,
	}}
	if argv, ok := e.relaunchedArgv(info.ID); ok {
		env.Argv = argv
	}
	if ep, ok := e.endpointFor(info.ID); ok {
		env.Endpoint = ep.url()
		env.Auth = ep.auth()
		env.Vars["port"], env.Vars["socket"] = strconv.Itoa(ep.port), ep.socket
	}
	if e.cfg.PanePIDs != nil {
		id := info.ID
		env.Procs = procs.Session{Panes: func(ctx context.Context) ([]int, error) { return e.cfg.PanePIDs(ctx, id) }}
	}
	if e.cfg.LoginPath != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		env.Vars[source.VarPath] = e.cfg.LoginPath(ctx)
		cancel()
	}
	if e.cfg.LoginEnv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		env.Environ = e.cfg.LoginEnv(ctx)
		cancel()
	}
	return env
}

var nonKey = regexp.MustCompile(`[^A-Za-z0-9]`)

// CwdKey is a folder as Claude Code and omp name their per-project
// folders: every character other than a letter or digit becomes "-".
func CwdKey(dir string) string { return nonKey.ReplaceAllString(filepath.Clean(dir), "-") }

// begin starts the adapter once.
func (s *sess) begin() {
	if s.ad != nil {
		s.once.Do(func() { go s.start() })
	}
}

// gateRetries are the pauses before asking a version command again after
// it failed to run. A loaded machine or a login PATH still being read can
// fail the first probe; one bad probe would otherwise leave the whole
// session in the terminal.
var gateRetries = []time.Duration{2 * time.Second, 8 * time.Second}

// checkGate runs the version gate, asking again after a transient failure.
func (s *sess) checkGate() gateResult {
	for i := 0; ; i++ {
		ctx, cancel := context.WithTimeout(s.ctx, gateTimeout+time.Second)
		r := s.e.gate.check(ctx, s.ad, s.env)
		cancel()
		if !r.transient || i >= len(gateRetries) || s.ctx.Err() != nil {
			return r
		}
		select {
		case <-s.ctx.Done():
			return r
		case <-time.After(gateRetries[i]):
		}
		if s.e.cfg.LoginPath != nil {
			lctx, lcancel := context.WithTimeout(s.ctx, 5*time.Second)
			if p := s.e.cfg.LoginPath(lctx); p != "" {
				// A new map: readers that copied s.env keep the old one.
				s.mu.Lock()
				vars := make(map[string]string, len(s.env.Vars))
				for k, v := range s.env.Vars {
					vars[k] = v
				}
				vars[source.VarPath] = p
				s.env.Vars = vars
				s.mu.Unlock()
			}
			lcancel()
		}
	}
}

func (s *sess) start() {
	defer close(s.ready)
	r := s.checkGate()
	s.mu.Lock()
	s.version, s.gateOK, s.gateWhy = r.version, r.ok, r.reason
	if !r.ok {
		for _, c := range s.ad.Capabilities {
			s.degraded[c] = true
		}
		s.mu.Unlock()
		if r.err != nil {
			s.e.cfg.Logf("harness: %s: %s (%v); the session shows the terminal", s.ad.Adapter.Adapter, r.reason, r.err)
		} else {
			s.e.cfg.Logf("harness: %s: %s; the session shows the terminal", s.ad.Adapter.Adapter, r.reason)
		}
		return
	}
	for _, name := range s.ad.feedOrder {
		mname := s.ad.Feeds[name].Mapper
		if _, ok := s.mappers[mname]; !ok {
			f, _ := harness.LookupMapper(mname)
			s.mappers[mname] = f(s.env)
		}
	}
	s.storeSummaryLocked()
	s.syncFeedsLocked()
	s.mu.Unlock()
	go s.seedAtStart()
	if s.e.cfg.OnSummary != nil {
		s.e.cfg.OnSummary(s.id)
	}
	s.e.startBuiltins(s)
}

func (s *sess) stop() {
	s.cancel()
	s.mu.Lock()
	for _, q := range s.subs {
		q.close()
	}
	s.subs = map[harness.Subscriber]*subQueue{}
	if s.summaryT != nil {
		s.summaryT.Stop()
	}
	s.mu.Unlock()
}

func (s *sess) waitReady(ctx context.Context) {
	select {
	case <-s.ready:
	case <-ctx.Done():
	case <-time.After(gateTimeout + 2*time.Second):
	}
}

func (s *sess) gateFailed() bool {
	select {
	case <-s.ready:
	default:
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.gateOK
}

func (s *sess) gateReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gateWhy
}

func (s *sess) degradedList() []string {
	out := []string{}
	for _, c := range s.ad.Capabilities {
		if s.degraded[c] {
			out = append(out, c)
		}
	}
	return out
}

// carry subscribes the subscribers of the session's previous run (a
// relaunch restarted it): items flow on as they did, the log and its epoch
// being the same, and a reset tells each to reopen for a fresh mission
// snapshot, since the new run builds its mission anew.
func (s *sess) carry(subs []harness.Subscriber) {
	if len(subs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range subs {
		select {
		case <-sub.Done():
			continue
		default:
		}
		if _, ok := s.subs[sub]; ok {
			continue
		}
		q := newSubQueue(sub, s.id, func() string { return s.e.log.Epoch(s.id) }, s.e.cfg.Now, func() { s.dropSub(sub) })
		s.subs[sub] = q
		q.release()
	}
	s.notifyLocked(harness.Notification{Kind: harness.NoteReset, Reason: "adapter"})
}

func (s *sess) dropSub(sub harness.Subscriber) {
	s.mu.Lock()
	q, ok := s.subs[sub]
	delete(s.subs, sub)
	s.mu.Unlock()
	if ok {
		q.close()
	}
}

// stateLocked merges the mappers' state in feed order: later non-empty
// values win, except working, which harness.MergeWorking decides, ORed
// with the hub's status.
func (s *sess) stateLocked() map[string]string {
	out, states := s.mapperStateLocked()
	working := harness.MergeWorking(states...)
	failed := s.turnFailedLocked(out["turn"], working)
	if failed {
		working = false // the screen ended a turn its hooks never closed
	}
	known := false
	for _, st := range states {
		if _, ok := st["working"]; ok {
			known = true
		}
	}
	now := s.e.cfg.Now()
	if known && s.feedWorking && !working {
		s.feedEndedAt = now
	}
	if known {
		s.feedWorking = working
	}
	// The hub follows the pane and settles seconds after the feed saw the
	// turn end; for a while after that end, the feed's word stands.
	feedEnded := known && !working && !s.feedEndedAt.IsZero() && now.Sub(s.feedEndedAt) < feedTrust
	status := s.hubStatus()
	switch {
	case agentGone(status):
		// A crashed or killed agent writes no turn end; its mappers would
		// say working forever.
		working = false
	case status == "working" && !s.holdingStopLocked(states) && !feedEnded && !failed:
		working = true
	}
	out["working"] = fmt.Sprint(working)
	out["idle"] = fmt.Sprint(!working)
	return out
}

// turnLocked reports whether the mappers see a turn running, and whether
// any of them reports one at all. Unlike stateLocked it leaves out the
// hub's status, which follows the pane and settles seconds after a turn
// ends.
func (s *sess) turnLocked() (working, known bool) {
	_, states := s.mapperStateLocked()
	for _, st := range states {
		if _, ok := st["working"]; ok {
			known = true
		}
	}
	return harness.MergeWorking(states...), known
}

// mapperStateLocked merges the mappers' state but working, and returns
// each mapper's state for harness.MergeWorking.
func (s *sess) mapperStateLocked() (map[string]string, []map[string]string) {
	out := map[string]string{}
	var states []map[string]string
	seen := map[string]bool{}
	for _, name := range s.ad.feedOrder {
		mname := s.ad.Feeds[name].Mapper
		if seen[mname] {
			continue
		}
		seen[mname] = true
		m := s.mappers[mname]
		if m == nil {
			continue
		}
		st := m.State()
		states = append(states, st)
		for k, v := range st {
			if k == "working" || k == harness.StateWorkingAt {
				continue
			}
			if v != "" {
				out[k] = v
			}
		}
	}
	s.seed.overlay(out)
	return out, states
}

// templateData is what adapter templates see.
func (s *sess) templateDataLocked(args map[string]any) map[string]any {
	data := map[string]any{}
	for k, v := range s.env.Vars {
		data[k] = v
	}
	data["state"] = stateAny(s.stateLocked())
	for k, v := range args {
		if _, taken := data[k]; taken || reservedVars[k] {
			// Phone input never replaces what the helper knows about
			// the session (its folder, endpoint, state).
			continue
		}
		data[k] = v
	}
	return data
}

// reservedVars are template names only the helper sets, even when a
// session has no value for them.
var reservedVars = map[string]bool{
	"home": true, "cwd": true, "cwd_key": true, "session": true, "created": true,
	"port": true, "socket": true, "path": true, "state": true, "endpoint": true,
}

var stateRef = regexp.MustCompile(`\.state\.([A-Za-z0-9_]+)`)

// syncFeedsLocked starts feeds whose templates can now render, and
// restarts a feed whose rendered block changed (a new transcript path).
func (s *sess) syncFeedsLocked() {
	state := s.stateLocked()
	data := s.templateDataLocked(nil)
	for _, name := range s.ad.feedOrder {
		f := s.ad.Feeds[name]
		raw, err := rawOf(f.Kinds)
		if err != nil {
			continue
		}
		ready := true
		for _, m := range stateRef.FindAllStringSubmatch(string(raw), -1) {
			if state[m[1]] == "" {
				ready = false
			}
		}
		run := s.feeds[name]
		if !ready {
			continue
		}
		rendered, err := RenderJSON(raw, data)
		if err != nil {
			s.e.cfg.Logf("harness: %s feed %s: %v", s.ad.Adapter.Adapter, name, err)
			continue
		}
		if run != nil && run.rendered == string(rendered) {
			continue
		}
		if run != nil {
			run.cancel()
		}
		ctx, cancel := context.WithCancel(s.ctx)
		run = &feedRun{name: name, rendered: string(rendered), cancel: cancel}
		s.feeds[name] = run
		go s.runFeed(ctx, run, harness.SourceSpec{Name: name, Kind: kindOf(f.Kinds), Raw: rendered}, f.Mapper)
	}
}

// runFeed keeps one feed running, retrying with backoff. Missing files are
// waited for; three failures in a row degrade the feed's capabilities.
func (s *sess) runFeed(ctx context.Context, run *feedRun, spec harness.SourceSpec, mapper string) {
	r, ok := harness.LookupRunner(spec.Kind)
	if !ok {
		return
	}
	backoff := time.Second
	for {
		err := r.Watch(ctx, spec, s.env, func(rec harness.Record) { s.record(ctx, mapper, rec) })
		if ctx.Err() != nil || err == nil {
			return
		}
		missing := isNotExist(err)
		s.mu.Lock()
		if !missing {
			run.failures++
			if run.failures == 3 {
				s.e.cfg.Logf("harness: %s feed %s (%s) failed three times: %v", s.ad.Adapter.Adapter, spec.Name, s.ad.Feeds[spec.Name].Stability, err)
				s.degradeLocked(feedCaps(spec.Kind))
			}
		}
		s.mu.Unlock()
		wait := backoff
		if missing {
			wait = time.Second
		} else if backoff < 10*time.Second {
			backoff *= 2
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func isNotExist(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "nothing matches"))
}

// feedCaps are the capabilities a failed feed of a kind takes down.
func feedCaps(kind string) []string {
	switch kind {
	case "statusline":
		return []string{harness.CapStatus, harness.CapUsage, harness.CapContext}
	case "hook":
		return nil
	}
	return []string{harness.CapHistory, harness.CapChatStream}
}

func (s *sess) degradeLocked(caps []string) {
	changed := false
	for _, c := range caps {
		if harness.Has(s.ad.Capabilities, c) && !s.degraded[c] {
			s.degraded[c] = true
			changed = true
		}
	}
	if changed {
		s.notifyLocked(harness.Notification{Kind: harness.NoteReset, Reason: "adapter"})
	}
}

// record maps one record and publishes the result.
func (s *sess) record(ctx context.Context, mapper string, rec harness.Record) {
	rec.Data = s.e.red.JSON(rec.Data)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	s.last[rec.Source] = rec.Data
	m := s.mappers[mapper]
	if m == nil {
		return
	}
	items, ops := safeMap(m, rec, s.e.cfg.Logf)
	s.warnPlanLocked(m)
	s.publishLocked(items, ops)
	if s.needs.hint {
		s.hookFiredLocked()
	}
	s.afterRecordLocked()
	if run := s.feeds[rec.Source]; run != nil {
		run.failures = 0
	}
}

// warnPlanLocked logs once per session that the mapper met a todo or plan
// call it could not read. It names the adapter and the agent's version and
// nothing the agent wrote.
func (s *sess) warnPlanLocked(m harness.Mapper) {
	w, ok := m.(harness.PlanWatcher)
	if !ok || s.planWarned || !w.PlanUnrecognised() {
		return
	}
	s.planWarned = true
	ver := s.version
	if ver == "" {
		ver = "unknown"
	}
	s.e.cfg.Logf("harness: unrecognised todo format (adapter %s, agent version %s)", s.ad.Adapter.Adapter, ver)
}

func safeMap(m harness.Mapper, rec harness.Record, logf func(string, ...any)) (items []harness.Item, ops []harness.Op) {
	defer func() {
		if r := recover(); r != nil {
			logf("harness: mapper %s panicked on a %s record: %v", m.Name(), rec.Source, r)
			items, ops = nil, nil
		}
	}()
	return m.Map(rec)
}

// afterRecordLocked reacts to state changes: feeds that depend on state,
// the working flag, the queue.
func (s *sess) afterRecordLocked() {
	s.syncFeedsLocked()
	st := s.stateLocked()
	if t := st["turn"]; t != "" {
		s.turn = t
	}
	s.setWorkingLocked(st["working"] == "true")
	s.refreshStatusLocked()
}

// statusChanged re-reads whether a turn runs once the hub's status moved.
// Working ORs in the hub, so a turn's last record can arrive while the hub
// still says working; without this the flag would stay set until the next
// record, showing Working, Interrupt and Queue on an idle session.
func (s *sess) statusChanged() {
	select {
	case <-s.ready:
	default:
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.gateOK {
		return
	}
	// A feed whose path comes from state (omp's session folder, found
	// once omp makes it) re-renders here too: waiting on a folder that
	// does not exist, it reads no record that would do it.
	s.syncFeedsLocked()
	s.setWorkingLocked(s.stateLocked()["working"] == "true")
	if agentGone(s.hubStatus()) {
		s.agentExitedLocked()
		s.exitNoticeLocked()
	} else {
		s.checkStuckTurnLocked()
		s.exitNoticed = false
	}
}

// feedTrust is how long a turn end the feed reported outweighs the hub's
// lingering "working" (the pane settles 6-10 s after a turn ends).
const feedTrust = 20 * time.Second

// turnSettle is how long after a turn ends its done push and its
// missing-reply check wait: the pane can say idle seconds before the
// feed's records of the reply arrive (omp writes its session file late).
var turnSettle = 6 * time.Second

// afterTurnLocked runs the done push and the missing-reply check once
// the turn's records had time to arrive, unless a new turn began.
func (s *sess) afterTurnLocked() {
	if s.e.cfg.Done == nil && !s.awaitReply {
		return
	}
	time.AfterFunc(s.e.scaled(turnSettle), func() {
		// The screen is read before the lock: it may say why no reply came.
		screen := s.screenNow()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.working || s.ctx == nil || s.ctx.Err() != nil {
			return
		}
		s.doneLocked()
		s.replyMissingLocked(screen)
	})
}

// doneLocked reports a finished turn with the last reply's text, unless
// the session's hooks are delivering (they send their own Stop "done").
// An adapter's hook feed alone is not enough: Codex 0.167 runs hooks only
// once the user trusts them, and hooks may not be installed at all.
func (s *sess) doneLocked() {
	if s.e.cfg.Done == nil || agentGone(s.hubStatus()) || s.needsOpenLocked() {
		// A turn that "ended" while a question or approval waits is
		// waiting on the user, not done.
		return
	}
	for name, f := range s.ad.Feeds {
		if f.Hook != nil && s.last[name] != nil {
			return
		}
	}
	recent, _, _ := s.e.log.Since(s.id, 0, 50)
	body := ""
	for i := len(recent) - 1; i >= 0; i-- {
		it := recent[i]
		if it.Kind == harness.KindUser {
			break // only this turn's reply
		}
		if it.Kind == harness.KindAssistant && it.Assistant != nil && it.Parent == "" {
			body = clipRunes(strings.TrimSpace(it.Assistant.Text), wire.MaxBody)
			break
		}
	}
	id, title := s.id, s.ad.Name+" finished"
	go s.e.cfg.Done(id, title, body)
}

// replyMissingLocked runs when a turn ends: if the phone's prompt got
// nothing back (no reply, tool, notice or question), say so in the chat.
// Seen on OpenCode with its model server down and on Vibe: the turn ran,
// ended, and the chat showed the prompt and nothing else.
//
// When the pane shows an error the agent printed ("litellm.APIConnectionError:
// …", "Please answer with one of: …"), the notice carries it and its reason.
func (s *sess) replyMissingLocked(screen string) {
	if !s.awaitReply || !harness.Has(s.ad.Capabilities, harness.CapChat) || agentGone(s.hubStatus()) {
		return
	}
	s.awaitReply = false
	now := s.e.cfg.Now()
	notice := &harness.NoticeBody{Level: "error", Code: harness.NoticeError,
		Text: s.ad.Name + " ended the turn without a reply. The terminal may show why."}
	if line, ok := screenError(screen, s.awaitText); ok {
		notice = harness.ErrorNotice(harness.ClassifyError(line), line)
	}
	s.publishLocked([]harness.Item{{ID: fmt.Sprintf("noreply-%d", now.UnixMilli()), TurnID: s.turn, Kind: harness.KindNotice, At: now,
		Notice: notice}}, nil)
}

// hubStatus is the hub's status for the session, "" when unknown.
func (s *sess) hubStatus() string {
	if st := s.e.cfg.Status; st != nil {
		return st(s.id)
	}
	return ""
}

// agentGone reports whether a hub status says the agent process exited.
func agentGone(status string) bool {
	return status == wire.StatusEnded || status == wire.StatusFailed || status == "exited"
}

// agentExitedLocked closes what the dead agent left open: running turns
// and tools end as interrupted, and prompts queued behind them are
// withdrawn, since nothing will ever drain them.
func (s *sess) agentExitedLocked() {
	recent, _, _ := s.e.log.Since(s.id, 0, 200)
	now := s.e.cfg.Now()
	var ended []harness.Item
	for _, it := range recent {
		switch {
		case it.Kind == harness.KindTurn && it.Turn != nil && it.Turn.State == harness.StateRunning:
			t := *it.Turn
			t.State, t.Ended = harness.StateInterrupted, &now
			it.Turn = &t
		case it.Kind == harness.KindTool && it.Tool != nil && it.Tool.State == harness.StateRunning:
			t := *it.Tool
			t.State, t.Ended = harness.StateInterrupted, &now
			it.Tool = &t
		default:
			continue
		}
		it.Seq = 0
		ended = append(ended, it)
	}
	for _, q := range s.queue {
		if q.item.User == nil {
			continue
		}
		it := q.item
		u := *it.User
		u.Queued, u.Withdrawn = false, true
		it.User = &u
		ended = append(ended, it)
	}
	s.queue = nil
	if len(ended) > 0 {
		s.publishLocked(ended, nil)
	}
}

// closeIdleLocked publishes what IdleCloser mappers close once the session
// went idle.
func (s *sess) closeIdleLocked() {
	now := s.e.cfg.Now()
	for _, m := range s.mappers {
		if c, ok := m.(harness.IdleCloser); ok {
			if items := c.CloseIdle(now); len(items) > 0 {
				s.publishLocked(items, nil)
			}
		}
	}
}

func (s *sess) setWorkingLocked(w bool) {
	was := s.working
	s.working = w
	if d := s.mission.setWorking(w); d != nil {
		s.missionDeltaLocked(d)
	}
	if was && !w {
		s.closeIdleLocked()
		s.afterTurnLocked()
		s.e.turnEnded(s)
		s.drainLocked()
		go s.seedFromScreen(seedSettle)
	}
}
