package awake

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

type fakeHolder struct {
	held             bool
	holds, releases  int
	holdErr, exitErr error
}

func (f *fakeHolder) Hold() error {
	if f.holdErr != nil {
		return f.holdErr
	}
	if !f.held {
		f.holds++
	}
	f.held = true
	f.exitErr = nil
	return nil
}

func (f *fakeHolder) Release() error {
	if f.held {
		f.releases++
	}
	f.held = false
	return nil
}

func (f *fakeHolder) Err() error { return f.exitErr }

type fakeOverride struct {
	on    bool
	calls []bool
	err   error
}

func (f *fakeOverride) Set(_ context.Context, on bool) error {
	f.calls = append(f.calls, on)
	if f.err != nil {
		return f.err
	}
	f.on = on
	return nil
}

type fakePower struct{ p Power }

func (f *fakePower) Read(context.Context) (Power, error) { return f.p, nil }

type recorder struct {
	mu     sync.Mutex
	events []Status
}

func (r *recorder) Publish(kind string, sessionID *string, payload any) {
	if kind != EventKind || sessionID != nil {
		return
	}
	r.mu.Lock()
	r.events = append(r.events, payload.(Event).Awake)
	r.mu.Unlock()
}

type rig struct {
	m        *Manager
	home     string
	holder   *fakeHolder
	override *fakeOverride
	power    *fakePower
	pub      *recorder
	now      time.Time
	admin    bool
}

func newRig(t *testing.T, mode Mode) *rig {
	t.Helper()
	r := &rig{
		home:     t.TempDir(),
		holder:   &fakeHolder{},
		override: &fakeOverride{},
		power:    &fakePower{p: Power{Source: PowerAC, Percent: intp(90)}},
		pub:      &recorder{},
		now:      time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC),
		admin:    true,
	}
	if err := SaveSettings(r.home, Settings{Mode: mode, BatteryFloor: DefaultBatteryFloor}); err != nil {
		t.Fatal(err)
	}
	m, err := New(Config{
		Home: r.home, Supported: true, Holder: r.holder, Override: r.override,
		AdminReady: func() bool { return r.admin }, Power: r.power, Pub: r.pub,
		Now: func() time.Time { return r.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	r.m = m
	return r
}

func intp(n int) *int { return &n }

func (r *rig) session(id, status string) {
	r.m.Observe(wire.Event{Kind: wire.EventSession, SessionID: &id, Payload: wire.SessionEvent{
		Session: session.Session{ID: id, Status: status, Managed: true},
	}})
}

func (r *rig) removed(id string) {
	r.m.Observe(wire.Event{Kind: wire.EventSessionRemoved, SessionID: &id, Payload: wire.SessionRemovedEvent{}})
}

func (r *rig) update() Status { return r.m.Update(context.Background()) }

func (r *rig) advance(d time.Duration) { r.now = r.now.Add(d) }

func TestHeldWhileWorkingReleasedAfterGrace(t *testing.T) {
	r := newRig(t, ModePlugged)
	if st := r.update(); st.Held || r.holder.held || st.Reason != ReasonIdle {
		t.Fatalf("no sessions: %+v, holder %v", st, r.holder.held)
	}
	r.session("s_aaaaaaaaaa", "working")
	if st := r.update(); !st.Held || !r.holder.held || st.Reason != ReasonActive {
		t.Fatalf("working session: %+v", st)
	}
	r.session("s_aaaaaaaaaa", "idle")
	r.advance(Grace - time.Second)
	if st := r.update(); !st.Held || st.Reason != ReasonGrace {
		t.Fatalf("inside grace: %+v", st)
	}
	r.advance(time.Second)
	if st := r.update(); st.Held || r.holder.held || st.Reason != ReasonIdle {
		t.Fatalf("after grace: %+v", st)
	}
	if r.holder.holds != 1 || r.holder.releases != 1 {
		t.Errorf("holds %d releases %d, want 1 and 1", r.holder.holds, r.holder.releases)
	}
}

func TestGraceRestartsWhenSessionWorksAgain(t *testing.T) {
	r := newRig(t, ModePlugged)
	r.session("s_aaaaaaaaaa", "working")
	r.update()
	r.session("s_aaaaaaaaaa", "done")
	r.advance(90 * time.Second)
	r.update()
	r.session("s_aaaaaaaaaa", "working")
	r.update()
	r.session("s_aaaaaaaaaa", "idle")
	r.advance(90 * time.Second)
	if st := r.update(); !st.Held {
		t.Fatalf("grace counts from the last active moment: %+v", st)
	}
	if r.holder.holds != 1 {
		t.Errorf("holds = %d, want one continuous hold", r.holder.holds)
	}
}

func TestNeedsYouHoldsAndIdleDoesNot(t *testing.T) {
	r := newRig(t, ModePlugged)
	r.session("s_aaaaaaaaaa", "idle")
	r.session("s_bbbbbbbbbb", "exited")
	if st := r.update(); st.Held {
		t.Fatalf("idle and exited sessions held the machine: %+v", st)
	}
	r.session("s_bbbbbbbbbb", "needs_you")
	if st := r.update(); !st.Held {
		t.Fatalf("a session waiting on the user did not hold: %+v", st)
	}
}

func TestUnmanagedSessionDoesNotHold(t *testing.T) {
	r := newRig(t, ModePlugged)
	id := "u_main"
	r.m.Observe(wire.Event{Kind: wire.EventSession, SessionID: &id, Payload: wire.SessionEvent{
		Session: session.Session{ID: id, Status: "working"},
	}})
	if st := r.update(); st.Held {
		t.Fatalf("unmanaged session held: %+v", st)
	}
}

func TestRemovedSessionStartsGrace(t *testing.T) {
	r := newRig(t, ModePlugged)
	r.session("s_aaaaaaaaaa", "working")
	r.update()
	r.removed("s_aaaaaaaaaa")
	if st := r.update(); st.Reason != ReasonGrace {
		t.Fatalf("after removal: %+v", st)
	}
	r.advance(Grace)
	if st := r.update(); st.Held {
		t.Fatalf("after grace: %+v", st)
	}
}

func TestOffNeverHolds(t *testing.T) {
	r := newRig(t, ModeOff)
	r.session("s_aaaaaaaaaa", "working")
	if st := r.update(); st.Held || r.holder.holds != 0 || st.Reason != ReasonOff {
		t.Fatalf("off: %+v, holds %d", st, r.holder.holds)
	}
	if len(r.override.calls) != 0 {
		t.Errorf("off touched disablesleep: %v", r.override.calls)
	}
}

func TestUnsupportedPlatform(t *testing.T) {
	m, err := New(Config{Home: t.TempDir(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	id := "s_aaaaaaaaaa"
	m.Observe(wire.Event{Kind: wire.EventSession, SessionID: &id, Payload: wire.SessionEvent{
		Session: session.Session{ID: id, Status: "working", Managed: true},
	}})
	st := m.Update(context.Background())
	if st.Held || st.PlatformSupported || st.Reason != ReasonUnsupported || st.Power != PowerUnknown {
		t.Fatalf("unsupported: %+v", st)
	}
}

func TestPluggedOnBatteryReportsLidSleeps(t *testing.T) {
	r := newRig(t, ModePlugged)
	r.power.p = Power{Source: PowerBattery, Percent: intp(60)}
	r.session("s_aaaaaaaaaa", "working")
	st := r.update()
	if !st.Held || st.Reason != ReasonOnBattery || st.Power != PowerBattery || st.BatteryPercent == nil || *st.BatteryPercent != 60 {
		t.Fatalf("plugged on battery: %+v", st)
	}
	if len(r.override.calls) != 0 {
		t.Errorf("plugged touched disablesleep: %v", r.override.calls)
	}
}

func TestHoldFailureReported(t *testing.T) {
	r := newRig(t, ModePlugged)
	r.holder.holdErr = errors.New("logind refused: access denied")
	r.session("s_aaaaaaaaaa", "working")
	st := r.update()
	if st.Held || st.Reason != ReasonFailed || st.Detail == "" {
		t.Fatalf("failed hold: %+v", st)
	}
	r.holder.holdErr = nil
	if st := r.update(); !st.Held || st.Reason != ReasonActive || st.Detail != "" {
		t.Fatalf("retry: %+v", st)
	}
	r.holder.held, r.holder.exitErr = false, errors.New("caffeinate exited")
	if st := r.update(); !st.Held || r.holder.holds != 2 {
		t.Fatalf("a child that died was not restarted: %+v, holds %d", st, r.holder.holds)
	}
}

func markerExists(home string) bool {
	_, err := os.Stat(OverrideMarker(home))
	return err == nil
}

func TestBatteryModeSetsAndClearsDisableSleep(t *testing.T) {
	r := newRig(t, ModeBattery)
	r.session("s_aaaaaaaaaa", "working")
	r.update()
	if !r.override.on || !markerExists(r.home) {
		t.Fatalf("active battery mode: override %v, marker %v", r.override.on, markerExists(r.home))
	}
	r.session("s_aaaaaaaaaa", "idle")
	r.advance(Grace)
	r.update()
	if r.override.on || markerExists(r.home) {
		t.Fatalf("idle: override %v, marker %v", r.override.on, markerExists(r.home))
	}
	if want := []bool{true, false}; !equalBools(r.override.calls, want) {
		t.Errorf("calls %v, want %v", r.override.calls, want)
	}
}

func TestBatteryModeWithoutAdminDoesNotTouchPmset(t *testing.T) {
	r := newRig(t, ModeBattery)
	r.admin = false
	r.session("s_aaaaaaaaaa", "working")
	st := r.update()
	if !st.Held || st.Reason != ReasonNeedsAdmin || !st.NeedsAdmin || len(r.override.calls) != 0 {
		t.Fatalf("battery without setup: %+v, calls %v", st, r.override.calls)
	}
}

func TestShutdownReleasesEverything(t *testing.T) {
	r := newRig(t, ModeBattery)
	r.session("s_aaaaaaaaaa", "working")
	r.update()
	r.m.Shutdown(context.Background())
	if r.holder.held || r.override.on || markerExists(r.home) {
		t.Fatalf("after shutdown: holder %v override %v marker %v", r.holder.held, r.override.on, markerExists(r.home))
	}
	r.m.Shutdown(context.Background())
	if want := []bool{true, false}; !equalBools(r.override.calls, want) {
		t.Errorf("calls %v, want %v", r.override.calls, want)
	}
}

func TestRecoverClearsOnlyWhatTogoSet(t *testing.T) {
	r := newRig(t, ModeBattery)
	r.m.Recover(context.Background())
	if len(r.override.calls) != 0 {
		t.Fatalf("no marker, yet disablesleep was touched: %v", r.override.calls)
	}
	if err := os.WriteFile(OverrideMarker(r.home), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.m.Recover(context.Background())
	if !equalBools(r.override.calls, []bool{false}) || markerExists(r.home) {
		t.Fatalf("marker left by a crash: calls %v, marker %v", r.override.calls, markerExists(r.home))
	}
}

func TestMarkerWrittenElsewhereIsCleared(t *testing.T) {
	r := newRig(t, ModeBattery)
	// `togo awake battery` sets disablesleep while checking sudo and
	// leaves the marker for the daemon.
	if err := os.WriteFile(OverrideMarker(r.home), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.update()
	if !equalBools(r.override.calls, []bool{false}) || markerExists(r.home) {
		t.Fatalf("calls %v, marker %v", r.override.calls, markerExists(r.home))
	}
}

func TestBatteryFloorWithHysteresis(t *testing.T) {
	r := newRig(t, ModeBattery)
	r.session("s_aaaaaaaaaa", "working")
	step := func(percent int, charging bool) Status {
		r.power.p = Power{Source: PowerBattery, Percent: intp(percent), Charging: charging}
		if charging {
			r.power.p.Source = PowerAC
		}
		r.advance(time.Minute) // past the power cache
		return r.update()
	}
	if st := step(40, false); !r.override.on || st.Reason != ReasonActive {
		t.Fatalf("40%%: %+v override %v", st, r.override.on)
	}
	if st := step(20, false); r.override.on || st.Reason != ReasonBatteryFloor || !st.Held {
		t.Fatalf("at floor: %+v override %v", st, r.override.on)
	}
	if st := step(25, false); r.override.on || st.Reason != ReasonBatteryFloor {
		t.Fatalf("floor+5 is not above it: %+v override %v", st, r.override.on)
	}
	if st := step(26, false); !r.override.on || st.Reason != ReasonActive {
		t.Fatalf("above floor+5: %+v override %v", st, r.override.on)
	}
	step(15, false)
	if st := step(15, true); !r.override.on {
		t.Fatalf("charging resumes: %+v override %v", st, r.override.on)
	}
}

func TestThermalReleasesOverrideOnBattery(t *testing.T) {
	r := newRig(t, ModeBattery)
	r.session("s_aaaaaaaaaa", "working")
	r.power.p = Power{Source: PowerAC, Percent: intp(80), Thermal: true}
	if st := r.update(); !r.override.on {
		t.Fatalf("thermal on AC keeps the override: %+v", st)
	}
	r.power.p = Power{Source: PowerBattery, Percent: intp(80), Thermal: true}
	r.advance(time.Minute)
	if st := r.update(); r.override.on || st.Reason != ReasonThermal || !st.Held {
		t.Fatalf("thermal on battery: %+v override %v", st, r.override.on)
	}
	r.power.p = Power{Source: PowerBattery, Percent: intp(80)}
	r.advance(time.Minute)
	if r.update(); !r.override.on {
		t.Fatal("override did not resume after the warning cleared")
	}
}

func TestSetBatteryNeedsAdminSetup(t *testing.T) {
	r := newRig(t, ModePlugged)
	r.admin = false
	if _, err := r.m.Set(context.Background(), Change{Mode: ptr(ModeBattery)}); !errors.Is(err, ErrNeedsAdminSetup) {
		t.Fatalf("battery without setup: err %v", err)
	}
	if s, _ := LoadSettings(r.home); s.Mode != ModePlugged {
		t.Fatalf("refused change was saved: %+v", s)
	}
	for _, m := range []Mode{ModeOff, ModePlugged} {
		st, err := r.m.Set(context.Background(), Change{Mode: ptr(m)})
		if err != nil || st.Mode != m {
			t.Fatalf("set %s: %+v %v", m, st, err)
		}
	}
	r.admin = true
	st, err := r.m.Set(context.Background(), Change{Mode: ptr(ModeBattery)})
	if err != nil || st.Mode != ModeBattery {
		t.Fatalf("battery after setup: %+v %v", st, err)
	}
	if s, _ := LoadSettings(r.home); s.Mode != ModeBattery {
		t.Fatalf("saved %+v", s)
	}
}

func TestSetBatteryFloorRange(t *testing.T) {
	r := newRig(t, ModePlugged)
	for _, bad := range []int{9, 51, -1} {
		if _, err := r.m.Set(context.Background(), Change{BatteryFloor: ptr(bad)}); err == nil {
			t.Errorf("floor %d accepted", bad)
		}
	}
	st, err := r.m.Set(context.Background(), Change{BatteryFloor: ptr(35)})
	if err != nil || st.BatteryFloor != 35 {
		t.Fatalf("floor 35: %+v %v", st, err)
	}
	if s, _ := LoadSettings(r.home); s.BatteryFloor != 35 || s.Mode != ModePlugged {
		t.Fatalf("saved %+v", s)
	}
}

func TestStatusPublishedOnChangeOnly(t *testing.T) {
	r := newRig(t, ModePlugged)
	r.update()
	r.update()
	r.session("s_aaaaaaaaaa", "working")
	r.update()
	r.update()
	if len(r.pub.events) != 2 || r.pub.events[0].Held || !r.pub.events[1].Held {
		t.Fatalf("events %+v, want idle then held", r.pub.events)
	}
}

func TestSettingsDefaultsAndBadFile(t *testing.T) {
	home := t.TempDir()
	if s, err := LoadSettings(home); err != nil || s.Mode != ModePlugged || s.BatteryFloor != DefaultBatteryFloor {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	if err := os.WriteFile(SettingsPath(home), []byte(`{"mode":"always","batteryFloor":90}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadSettings(home); err == nil || s.Mode != ModePlugged || s.BatteryFloor != DefaultBatteryFloor {
		t.Fatalf("bad file: %+v %v", s, err)
	}
}

func ptr[T any](v T) *T { return &v }

func equalBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFailedClearIsReportedNotIdle(t *testing.T) {
	r := newRig(t, ModePlugged)
	if err := os.WriteFile(OverrideMarker(r.home), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.override.err = errors.New("sudo: a password is required")
	st := r.update()
	if st.Reason != ReasonFailed || !strings.Contains(st.Detail, "password") || st.Held {
		t.Fatalf("failed clear: %+v", st)
	}
	if !markerExists(r.home) {
		t.Fatal("marker dropped although disablesleep is still set")
	}
	r.override.err = nil
	if st := r.update(); st.Reason != ReasonIdle || markerExists(r.home) {
		t.Fatalf("after a successful retry: %+v, marker %v", st, markerExists(r.home))
	}
}
