package awake

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	// Grace is how long the hold outlives the last active session, so a
	// session that pauses between turns does not make the machine flap.
	Grace = 2 * time.Minute
	// powerTTL is how long a power reading is reused.
	powerTTL = 20 * time.Second
	// tick is how often Run re-evaluates without a session change, to end
	// the grace period and follow the battery.
	tick = 10 * time.Second
	// resumeMargin is how far above the floor the battery must climb
	// before the override comes back.
	resumeMargin = 5
)

// Reasons in Status.Reason.
const (
	ReasonUnsupported  = "unsupported"
	ReasonOff          = "off"
	ReasonIdle         = "idle"
	ReasonActive       = "active"
	ReasonGrace        = "grace"
	ReasonOnBattery    = "on_battery"
	ReasonNeedsAdmin   = "needs_admin_setup"
	ReasonBatteryFloor = "battery_floor"
	ReasonThermal      = "thermal"
	ReasonFailed       = "failed"
)

// Status is the awake.status result and the awake event payload.
type Status struct {
	Mode   Mode   `json:"mode"`
	Held   bool   `json:"held"`
	Reason string `json:"reason"`
	// Detail says what failed when Reason is failed.
	Detail            string `json:"detail,omitempty"`
	Power             string `json:"power"`
	BatteryPercent    *int   `json:"batteryPercent,omitempty"`
	BatteryFloor      int    `json:"batteryFloor"`
	PlatformSupported bool   `json:"platformSupported"`
	NeedsAdmin        bool   `json:"needsAdmin"`
}

// EventKind is the kind of the event published when Status changes.
const EventKind = "awake"

// Event is the payload of an awake event.
type Event struct {
	Awake Status `json:"awake"`
}

// ErrNeedsAdminSetup refuses battery mode before its sudoers rule exists.
var ErrNeedsAdminSetup = errors.New("battery mode needs admin setup: run togo awake battery on this machine")

// Config wires a Manager.
type Config struct {
	Home string
	// Supported is false on platforms with no way to hold the machine.
	Supported bool
	Holder    Holder
	// Override is nil where battery mode needs nothing beyond Holder.
	Override SleepOverride
	// AdminReady reports whether Override can run unattended. Nil means
	// it needs no setup.
	AdminReady func() bool
	// Power is nil where the power source cannot be read.
	Power PowerReader
	Pub   wire.Publisher
	Now   func() time.Time
	Logf  func(format string, args ...any)
}

// Manager decides when to hold the machine and applies it.
type Manager struct {
	cfg Config

	// obsMu guards the session view, which Observe updates from the
	// hub's publishing goroutine and so must never wait on mu.
	obsMu     sync.Mutex
	active    map[string]bool
	idleSince time.Time
	poke      chan struct{}

	mu         sync.Mutex
	settings   Settings
	holding    bool
	holdErr    error
	overrideOn bool
	overErr    error
	belowFloor bool
	power      Power
	powerAt    time.Time
	last       *Status
	stopped    bool
}

// New loads the settings from cfg.Home. A bad settings file is logged and
// the defaults used.
func New(cfg Config) (*Manager, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	s, err := LoadSettings(cfg.Home)
	if err != nil {
		cfg.Logf("awake: settings: %v", err)
	}
	return &Manager{cfg: cfg, settings: s, active: map[string]bool{}, poke: make(chan struct{}, 1)}, nil
}

// Observe follows the hub's events: a managed session that is working or
// waiting on the user keeps the machine awake.
func (m *Manager) Observe(ev wire.Event) {
	if ev.SessionID == nil {
		return
	}
	var active, known bool
	switch ev.Kind {
	case wire.EventSession:
		p, ok := ev.Payload.(wire.SessionEvent)
		if !ok {
			return
		}
		s, ok := p.Session.(session.Session)
		if !ok {
			return
		}
		active, known = s.Managed && (s.Status == "working" || s.Status == "needs_you"), true
	case wire.EventSessionRemoved:
		known = true
	}
	if !known {
		return
	}
	m.obsMu.Lock()
	was := len(m.active) > 0
	if active {
		m.active[*ev.SessionID] = true
	} else {
		delete(m.active, *ev.SessionID)
	}
	changed := was != (len(m.active) > 0)
	if was && len(m.active) == 0 {
		m.idleSince = m.cfg.Now()
	}
	m.obsMu.Unlock()
	if changed {
		select {
		case m.poke <- struct{}{}:
		default:
		}
	}
}

// activity reports whether any session is active and whether the grace
// period after the last one is still running.
func (m *Manager) activity(now time.Time) (active, grace bool) {
	m.obsMu.Lock()
	defer m.obsMu.Unlock()
	if len(m.active) > 0 {
		return true, false
	}
	return false, !m.idleSince.IsZero() && now.Sub(m.idleSince) < Grace
}

// Change is an awake.set request. Nil fields are left as they are.
type Change struct {
	Mode         *Mode `json:"mode"`
	BatteryFloor *int  `json:"batteryFloor"`
}

// Set saves a change and applies it. Battery mode is refused with
// ErrNeedsAdminSetup until its admin setup is done.
func (m *Manager) Set(ctx context.Context, c Change) (Status, error) {
	m.mu.Lock()
	next := m.settings
	if c.Mode != nil {
		next.Mode = *c.Mode
	}
	if c.BatteryFloor != nil {
		if err := ValidateFloor(*c.BatteryFloor); err != nil {
			m.mu.Unlock()
			return Status{}, err
		}
		next.BatteryFloor = *c.BatteryFloor
	}
	if next.Mode == ModeBattery && m.settings.Mode != ModeBattery && m.needsAdmin() {
		m.mu.Unlock()
		return Status{}, ErrNeedsAdminSetup
	}
	if err := SaveSettings(m.cfg.Home, next); err != nil {
		m.mu.Unlock()
		return Status{}, err
	}
	m.settings = next
	m.mu.Unlock()
	return m.Update(ctx), nil
}

// Status returns the last evaluated status, evaluating once if needed.
func (m *Manager) Status(ctx context.Context) Status {
	m.mu.Lock()
	last := m.last
	m.mu.Unlock()
	if last != nil {
		return *last
	}
	return m.Update(ctx)
}

func (m *Manager) needsAdmin() bool {
	return m.cfg.Override != nil && m.cfg.AdminReady != nil && !m.cfg.AdminReady()
}

func (m *Manager) readPower(ctx context.Context, now time.Time) Power {
	if m.cfg.Power == nil {
		return Power{Source: PowerUnknown}
	}
	if !m.powerAt.IsZero() && now.Sub(m.powerAt) < powerTTL {
		return m.power
	}
	p, err := m.cfg.Power.Read(ctx)
	if err != nil {
		m.cfg.Logf("awake: power: %v", err)
		p = Power{Source: PowerUnknown}
	}
	m.power, m.powerAt = p, now
	return p
}

// Update evaluates the sessions, settings and power, holds or releases the
// machine to match, and publishes the status if it changed.
func (m *Manager) Update(ctx context.Context) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return *m.last
	}
	now := m.cfg.Now()
	active, grace := m.activity(now)
	s := m.settings
	st := Status{
		Mode: s.Mode, BatteryFloor: s.BatteryFloor, PlatformSupported: m.cfg.Supported,
		NeedsAdmin: m.needsAdmin(), Power: PowerUnknown,
	}
	var pw Power
	if m.cfg.Supported {
		pw = m.readPower(ctx, now)
		st.Power, st.BatteryPercent = pw.Source, pw.Percent
	}
	want := m.cfg.Supported && s.Mode != ModeOff && (active || grace)

	m.applyHold(want)

	discharging := pw.Source == PowerBattery && !pw.Charging
	switch {
	case discharging && pw.Percent != nil && *pw.Percent <= s.BatteryFloor:
		m.belowFloor = true
	case !discharging || (pw.Percent != nil && *pw.Percent > s.BatteryFloor+resumeMargin):
		m.belowFloor = false
	}
	thermal := pw.Source == PowerBattery && pw.Thermal
	wantOverride := want && s.Mode == ModeBattery && m.cfg.Override != nil && !st.NeedsAdmin && !m.belowFloor && !thermal
	m.applyOverride(ctx, wantOverride)

	st.Held = m.holding
	switch {
	case !m.cfg.Supported:
		st.Reason = ReasonUnsupported
	case m.overErr != nil:
		// Shown whatever the mode: a disablesleep that cannot be cleared
		// keeps the machine awake with nothing active.
		st.Reason, st.Detail = ReasonFailed, "disablesleep: "+m.overErr.Error()
	case s.Mode == ModeOff:
		st.Reason = ReasonOff
	case !want:
		st.Reason = ReasonIdle
	case m.holdErr != nil:
		st.Reason, st.Detail = ReasonFailed, m.holdErr.Error()
	case s.Mode == ModeBattery && m.cfg.Override != nil && st.NeedsAdmin:
		st.Reason = ReasonNeedsAdmin
	case s.Mode == ModeBattery && m.belowFloor:
		st.Reason = ReasonBatteryFloor
	case s.Mode == ModeBattery && thermal:
		st.Reason = ReasonThermal
	case s.Mode == ModePlugged && pw.Source == PowerBattery:
		st.Reason = ReasonOnBattery
	case !active:
		st.Reason = ReasonGrace
	default:
		st.Reason = ReasonActive
	}
	m.publish(st)
	return st
}

func (m *Manager) applyHold(want bool) {
	h := m.cfg.Holder
	if h == nil {
		return
	}
	if m.holding {
		if err := h.Err(); err != nil {
			m.holding = false
			m.setHoldErr(err)
		}
	}
	switch {
	case want && !m.holding:
		if err := h.Hold(); err != nil {
			m.setHoldErr(err)
			return
		}
		m.holding = true
		m.setHoldErr(nil)
	case !want:
		if m.holding {
			if err := h.Release(); err != nil {
				m.cfg.Logf("awake: release: %v", err)
			}
			m.holding = false
		}
		m.holdErr = nil
	}
}

// setHoldErr records a hold failure, logging it only when it changes so a
// retry every tick does not fill the log.
func (m *Manager) setHoldErr(err error) {
	if err != nil && (m.holdErr == nil || m.holdErr.Error() != err.Error()) {
		m.cfg.Logf("awake: hold: %v", err)
	}
	m.holdErr = err
}

// applyOverride sets or clears disablesleep. The marker is written before
// setting it, so a crash in between still leaves a record to clear; a
// marker written by `togo awake battery` counts as set.
func (m *Manager) applyOverride(ctx context.Context, want bool) {
	o := m.cfg.Override
	if o == nil {
		return
	}
	on := m.overrideOn || overrideMarked(m.cfg.Home)
	switch {
	case want && !on:
		if err := markOverride(m.cfg.Home); err != nil {
			m.overErr = err
			return
		}
		if err := o.Set(ctx, true); err != nil {
			m.cfg.Logf("awake: disablesleep 1: %v", err)
			m.overErr = err
			_ = unmarkOverride(m.cfg.Home)
			return
		}
		m.overrideOn, m.overErr = true, nil
	case !want && on:
		if err := o.Set(ctx, false); err != nil {
			m.cfg.Logf("awake: disablesleep 0: %v", err)
			m.overErr = err
			return
		}
		m.overrideOn, m.overErr = false, unmarkOverride(m.cfg.Home)
	case !want:
		m.overErr = nil
	}
}

func (m *Manager) publish(st Status) {
	if m.last != nil && reflect.DeepEqual(*m.last, st) {
		return
	}
	m.last = &st
	if m.cfg.Pub != nil {
		m.cfg.Pub.Publish(EventKind, nil, Event{Awake: st})
	}
}

// Recover clears a disablesleep left set by a previous daemon that did not
// stop cleanly. Only a setting Togo made (the marker exists) is cleared.
func (m *Manager) Recover(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.Override == nil {
		return
	}
	if cleared, err := ClearOverride(ctx, m.cfg.Home, m.cfg.Override); err != nil {
		m.cfg.Logf("awake: clear disablesleep left by an earlier run: %v", err)
	} else if cleared {
		m.cfg.Logf("awake: cleared disablesleep left by an earlier run")
	}
	m.overrideOn = false
}

// Shutdown releases the hold and clears disablesleep. Later updates do
// nothing. It is safe to call more than once.
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	if m.cfg.Holder != nil && m.holding {
		if err := m.cfg.Holder.Release(); err != nil {
			m.cfg.Logf("awake: release: %v", err)
		}
	}
	m.holding = false
	m.applyOverride(ctx, false)
	st := Status{Mode: m.settings.Mode, Reason: ReasonIdle, Power: PowerUnknown, BatteryFloor: m.settings.BatteryFloor, PlatformSupported: m.cfg.Supported}
	if m.last != nil {
		st = *m.last
		st.Held, st.Reason, st.Detail = false, ReasonIdle, ""
	}
	m.last = &st
	m.stopped = true
}

// Run clears what an earlier run left, then follows sessions and power
// until ctx ends, and shuts down. Callers that exit right after ctx ends
// also call Shutdown themselves, since Run's own cleanup may not get to
// run.
func (m *Manager) Run(ctx context.Context) {
	m.Recover(ctx)
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		m.Update(ctx)
		select {
		case <-ctx.Done():
			m.Shutdown(context.WithoutCancel(ctx))
			return
		case <-t.C:
		case <-m.poke:
		}
	}
}
