package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
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

	mu       sync.Mutex
	env      harness.Env
	version  string
	gateOK   bool
	gateWhy  string
	degraded map[string]bool
	mappers  map[string]harness.Mapper // by mapper name, shared by feeds
	feeds    map[string]*feedRun
	last     map[string]json.RawMessage // latest record per feed
	anchors  map[string]*harness.Anchor
	mission  *missionState
	subs     map[harness.Subscriber]*subQueue
	queue    []queued
	sent     map[string]bool // queued prompts already typed, by item id
	phone    []phoneText
	working  bool
	turn     string
	summaryT *time.Timer
	srcRows  map[string]map[string]bool // mission rows each section's source listed
	// sum mirrors the mission summary for readers that must not take mu
	// (the hub decorates sessions while holding its own lock).
	sum      atomic.Pointer[harness.Summary]
	pending  map[string]harness.Item // coalesced partial updates
	lastPart map[string]time.Time
}

type queued struct {
	item harness.Item
}

// phoneText is a prompt typed from the phone, waiting for the agent's own
// record of it.
type phoneText struct {
	id   string
	text string
	at   time.Time
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
		subs: map[harness.Subscriber]*subQueue{}, pending: map[string]harness.Item{}, lastPart: map[string]time.Time{},
	}
	if info.Managed {
		for _, id := range sortedIDs(e.adapters) {
			if a := e.adapters[id]; a.matches(info.Agent, info.Command) {
				s.ad = a
				break
			}
		}
	}
	if s.ad == nil {
		close(s.ready)
		return s
	}
	s.env = e.envFor(info)
	s.mission = newMission(s.ad.Capabilities)
	return s
}

// envFor builds the template and runner environment of a session.
func (e *Engine) envFor(info session.Session) harness.Env {
	home := e.cfg.Home
	cwd := source.Expand(info.Cwd, home)
	env := harness.Env{SessionID: info.ID, Agent: info.Agent, Cwd: cwd, Argv: info.Command, Vars: map[string]string{
		source.VarHome: home, "cwd": cwd, "cwd_key": CwdKey(cwd), "session": info.ID,
		"created": info.CreatedAt.UTC().Format(time.RFC3339),
	}}
	if ep, ok := e.endpointFor(info.ID); ok {
		env.Endpoint = ep.url()
		env.Auth = ep.auth()
		env.Vars["port"], env.Vars["socket"] = strconv.Itoa(ep.port), ep.socket
	}
	if e.cfg.LoginPath != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		env.Vars[source.VarPath] = e.cfg.LoginPath(ctx)
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

func (s *sess) start() {
	defer close(s.ready)
	ctx, cancel := context.WithTimeout(s.ctx, gateTimeout+time.Second)
	r := s.e.gate.check(ctx, s.ad, s.env)
	cancel()
	s.mu.Lock()
	s.version, s.gateOK, s.gateWhy = r.version, r.ok, r.reason
	if !r.ok {
		for _, c := range s.ad.Capabilities {
			s.degraded[c] = true
		}
		s.mu.Unlock()
		s.e.cfg.Logf("harness: %s: %s; the session shows the terminal", s.ad.Adapter.Adapter, r.reason)
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
// values win, except working, which is true when any mapper says so.
func (s *sess) stateLocked() map[string]string {
	out := map[string]string{}
	working := false
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
		for k, v := range m.State() {
			if k == "working" {
				working = working || v == "true"
				continue
			}
			if v != "" {
				out[k] = v
			}
		}
	}
	if st := s.e.cfg.Status; st != nil && st(s.id) == "working" {
		working = true
	}
	out["working"] = fmt.Sprint(working)
	out["idle"] = fmt.Sprint(!working)
	return out
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
	s.publishLocked(items, ops)
	s.afterRecordLocked()
	if run := s.feeds[rec.Source]; run != nil {
		run.failures = 0
	}
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
	s.setWorkingLocked(s.stateLocked()["working"] == "true")
}

func (s *sess) setWorkingLocked(w bool) {
	was := s.working
	s.working = w
	if d := s.mission.setWorking(w); d != nil {
		s.missionDeltaLocked(d)
	}
	if was && !w {
		s.e.turnEnded(s)
		s.drainLocked()
	}
}
