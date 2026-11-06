package push

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// willEnv drives a WillKeeper by hand: events through the fake hub, time
// through clock, builds through tick.
type willEnv struct {
	*env
	keeper *WillKeeper
	muted  atomic.Bool
	now    time.Time
}

func newWillEnv(t *testing.T, devices int) *willEnv {
	t.Helper()
	w := &willEnv{env: newEnv(t, devices), now: time.UnixMilli(1793707200000)}
	w.keeper = &WillKeeper{
		Hub: w.hub, Devices: w.svc.Devices(), Keys: w.svc.HostKeys,
		Muted: w.muted.Load, Host: "studio.local",
		Now: func() time.Time { return w.now },
	}
	w.keeper.start(context.Background())
	return w
}

// advance moves the clock and runs one tick.
func (w *willEnv) advance(d time.Duration) {
	w.now = w.now.Add(d)
	w.keeper.tick()
}

func (w *willEnv) frame(t *testing.T) (willFrame, []byte) {
	t.Helper()
	raw, ok := w.keeper.Will(w.relay.URL + "/")
	if !ok {
		t.Fatal("no will yet")
	}
	var f willFrame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("frame %s: %v", raw, err)
	}
	return f, raw
}

func managedEv(id, status string) wire.Event { return sessionEv(id, "07", status) }

func TestNoWillWhenNothingActive(t *testing.T) {
	w := newWillEnv(t, 1)
	if _, ok := w.keeper.Will(w.relay.URL); ok {
		t.Fatal("will offered before the first build")
	}
	w.advance(willDebounce)
	_, raw := w.frame(t)
	if string(raw) != `{"type":"will","v":1,"pushes":[]}` {
		t.Fatalf("frame %s, want the disarm frame", raw)
	}
	// An idle session does not arm it either.
	w.hub.publish(managedEv("s_aaaaaaaaaa", "idle"))
	w.advance(willDebounce)
	if _, raw := w.frame(t); string(raw) != string(disarmFrame) {
		t.Fatalf("idle session armed the will: %s", raw)
	}
}

func TestWillArmsOnWorking(t *testing.T) {
	w := newWillEnv(t, 2)
	w.advance(willDebounce)
	changed := w.keeper.Changed()
	w.hub.publish(managedEv("s_aaaaaaaaaa", "working"))
	w.hub.publish(managedEv("s_bbbbbbbbbb", "needs_you"))
	w.advance(time.Second)
	w.advance(willDebounce)
	select {
	case <-changed:
	default:
		t.Fatal("Changed not signalled after the rebuild")
	}
	f, _ := w.frame(t)
	if f.Type != "will" || f.V != 1 || f.AfterMs != 300000 {
		t.Fatalf("frame header %+v", f)
	}
	if len(f.Pushes) != 2 {
		t.Fatalf("%d pushes, want one per device", len(f.Pushes))
	}

	raw, err := os.ReadFile("../../../protocol/v1/fixtures/plaintext/push.offline.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Plaintext map[string]any `json:"plaintext"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	keys, _ := w.svc.HostKeys()
	for i, ph := range w.phones {
		p := f.Pushes[i]
		if p.Kind != relayAlert || p.Token != strings.Repeat("ab", 32) || p.Env != wire.EnvSandbox || p.CollapseID != "offline" {
			t.Fatalf("push %d envelope %+v", i, p)
		}
		plain, err := openRaw(ph, p.Ciphertext, keys.HostID)
		if err != nil {
			t.Fatalf("device %s cannot open its push: %v", ph.id, err)
		}
		var got map[string]any
		if err := json.Unmarshal(plain, &got); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(sortedKeys(got), sortedKeys(fx.Plaintext)) {
			t.Fatalf("plaintext keys %v, fixture %v", sortedKeys(got), sortedKeys(fx.Plaintext))
		}
		for _, k := range []string{"v", "kind", "sessionId", "bib", "agent", "active", "host"} {
			if !reflect.DeepEqual(got[k], fx.Plaintext[k]) {
				t.Errorf("%s = %v, fixture %v", k, got[k], fx.Plaintext[k])
			}
		}
		// The app writes the words; the helper sends none.
		if got["title"] != "" || got["body"] != "" {
			t.Errorf("title %q body %q, want empty", got["title"], got["body"])
		}
		if got["ts"] != float64(w.now.UnixMilli()) {
			t.Errorf("ts %v, want seal time %d", got["ts"], w.now.UnixMilli())
		}
		// Sealed for this device only.
		other := w.phones[1-i]
		if _, err := openRaw(phone{id: ph.id, priv: other.priv}, p.Ciphertext, keys.HostID); err == nil {
			t.Error("push opens with another device's key")
		}
	}
}

func sortedKeys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestWillCountsOnlyManagedActive(t *testing.T) {
	w := newWillEnv(t, 1)
	ev := managedEv("s_aaaaaaaaaa", "working")
	pl := ev.Payload.(wire.SessionEvent)
	s, ok := sessionOf(pl.Session)
	if !ok {
		t.Fatal("no session in event")
	}
	s.Managed = false
	pl.Session = s
	ev.Payload = pl
	w.hub.publish(ev)
	w.advance(willDebounce)
	if _, raw := w.frame(t); string(raw) != string(disarmFrame) {
		t.Fatalf("unmanaged session armed the will: %s", raw)
	}
}

func TestWillDebounced(t *testing.T) {
	w := newWillEnv(t, 1)
	w.advance(willDebounce)
	base := w.keeper.rebuilds
	for i := range 5 {
		st := "working"
		if i%2 == 1 {
			st = "needs_you"
		}
		w.hub.publish(managedEv("s_aaaaaaaaaa", st))
		w.advance(2 * time.Second)
	}
	if n := w.keeper.rebuilds - base; n != 0 {
		t.Fatalf("%d rebuilds inside the debounce window", n)
	}
	w.advance(willDebounce)
	w.advance(willDebounce)
	if n := w.keeper.rebuilds - base; n != 1 {
		t.Fatalf("%d rebuilds after 5 flips, want 1", n)
	}
	if f, _ := w.frame(t); len(f.Pushes) != 1 {
		t.Fatalf("will not armed after the flips: %+v", f)
	}
	// No change, no rebuild, until the refresh.
	w.advance(time.Hour)
	if n := w.keeper.rebuilds - base; n != 1 {
		t.Fatalf("%d rebuilds without a change", n)
	}
	w.advance(willRefresh)
	if n := w.keeper.rebuilds - base; n != 2 {
		t.Fatalf("%d rebuilds after 6 h armed, want 2", n)
	}
}

func TestWillMutedDisarms(t *testing.T) {
	w := newWillEnv(t, 1)
	w.hub.publish(managedEv("s_aaaaaaaaaa", "working"))
	w.advance(willDebounce)
	if f, _ := w.frame(t); len(f.Pushes) != 1 {
		t.Fatalf("not armed: %+v", f)
	}
	w.muted.Store(true)
	w.advance(time.Second)
	w.advance(willDebounce)
	if _, raw := w.frame(t); string(raw) != string(disarmFrame) {
		t.Fatalf("muted machine still armed: %s", raw)
	}
	w.muted.Store(false)
	w.advance(time.Second)
	w.advance(willDebounce)
	if f, _ := w.frame(t); len(f.Pushes) != 1 {
		t.Fatalf("not armed again after unmute: %+v", f)
	}
}

func TestWillDisarmsWhenDevicesGo(t *testing.T) {
	w := newWillEnv(t, 1)
	w.hub.publish(managedEv("s_aaaaaaaaaa", "working"))
	w.advance(willDebounce)
	if err := w.svc.Devices().Remove("dev_a"); err != nil {
		t.Fatal(err)
	}
	w.advance(time.Second)
	w.advance(willDebounce)
	if _, raw := w.frame(t); string(raw) != string(disarmFrame) {
		t.Fatalf("will kept with no device: %s", raw)
	}
}

// The Watch shows the offline alert mirrored from the phone, so the will
// holds no push for it.
func TestWillSkipsTheWatch(t *testing.T) {
	w := newWillEnv(t, 1)
	w.pairWith(t, "w_watch", func(p *wire.PairParams) { p.Platform = wire.PlatformWatch })
	w.hub.publish(managedEv("s_aaaaaaaaaa", "working"))
	w.advance(willDebounce)
	f, _ := w.frame(t)
	if len(f.Pushes) != 1 || f.Pushes[0].Target != "" {
		t.Fatalf("pushes %+v, want the phone's alert only", f.Pushes)
	}
}

func TestWillLiveActivityMetadataOnly(t *testing.T) {
	w := newWillEnv(t, 0)
	live := strings.Repeat("cd", 32)
	w.pairWith(t, "dev_a", func(p *wire.PairParams) {
		p.LiveActivityToken = live
		p.LiveActivityCommand = true
	})
	w.hub.publish(managedEv("s_aaaaaaaaaa", "working"))
	w.hub.publish(managedEv("s_bbbbbbbbbb", "needs_you"))
	w.advance(willDebounce)
	f, raw := w.frame(t)
	if len(f.Pushes) != 2 {
		t.Fatalf("%d pushes, want alert and Live Activity", len(f.Pushes))
	}
	la := f.Pushes[1]
	if la.Kind != relayLiveActivity || la.Token != live || la.Ciphertext != "" || la.LiveActivity == nil {
		t.Fatalf("live push %+v", la)
	}
	st := la.LiveActivity.ContentState
	if la.LiveActivity.Event != liveUpdate || st.Status != "offline" || st.Waiting != 1 ||
		st.Since != w.now.Unix() || st.Command != nil || st.RequestID != nil || st.Agent != "codex" {
		t.Fatalf("content state %+v", st)
	}
	if la.LiveActivity.StaleDate != nil || la.LiveActivity.DismissalDate != nil {
		t.Fatalf("dates on the will's activity: %+v", la.LiveActivity)
	}
	if strings.Contains(string(raw), "staleDate") || !strings.Contains(string(raw), `"command":null`) {
		t.Fatalf("frame %s", raw)
	}
}

func TestWillFitsFrame(t *testing.T) {
	w := newWillEnv(t, 0)
	for i := range 8 {
		w.pairWith(t, "dev_"+string(rune('a'+i)), func(p *wire.PairParams) {
			p.APNsToken = strings.Repeat("a", 200)
			p.LiveActivityToken = strings.Repeat("b", 200)
		})
	}
	for i := range 5 {
		w.hub.publish(managedEv("s_aaaaaaaaa"+string(rune('a'+i)), "working"))
	}
	w.advance(willDebounce)
	f, raw := w.frame(t)
	if len(raw) > 32768 {
		t.Fatalf("frame is %d bytes", len(raw))
	}
	if !strings.HasPrefix(string(raw), `{"type":"will",`) {
		t.Fatalf("frame does not start with its type: %.40s", raw)
	}
	if len(f.Pushes) != 8 {
		t.Fatalf("%d pushes, want the relay's 8", len(f.Pushes))
	}
	for i, p := range f.Pushes {
		if p.Kind != relayAlert {
			t.Fatalf("push %d is %s; alerts must come first", i, p.Kind)
		}
	}
}

func TestWillPerRelay(t *testing.T) {
	w := newWillEnv(t, 1)
	other := newRelay(t)
	w.pairWith(t, "dev_b", func(p *wire.PairParams) { p.RelayURL = other.URL })
	w.hub.publish(managedEv("s_aaaaaaaaaa", "working"))
	w.advance(willDebounce)
	a, _ := w.frame(t)
	rawB, ok := w.keeper.Will(other.URL)
	var b willFrame
	if !ok || json.Unmarshal(rawB, &b) != nil {
		t.Fatalf("no will for the second relay: %s", rawB)
	}
	if len(a.Pushes) != 1 || len(b.Pushes) != 1 || a.Pushes[0].Ciphertext == b.Pushes[0].Ciphertext {
		t.Fatalf("relays share pushes: %d %d", len(a.Pushes), len(b.Pushes))
	}
	if raw, ok := w.keeper.Will("https://elsewhere.example"); !ok || string(raw) != string(disarmFrame) {
		t.Fatalf("unknown relay got %s", raw)
	}
}

func TestWillRetriedAfterKeyFailure(t *testing.T) {
	w := newWillEnv(t, 1)
	fail := true
	w.keeper.Keys = func() (HostKeys, error) {
		if fail {
			return HostKeys{}, os.ErrPermission
		}
		return w.svc.HostKeys()
	}
	w.hub.publish(managedEv("s_aaaaaaaaaa", "working"))
	w.advance(willDebounce)
	if _, ok := w.keeper.Will(w.relay.URL); ok {
		t.Fatal("will offered after a failed build")
	}
	fail = false
	w.advance(willDebounce)
	if f, _ := w.frame(t); len(f.Pushes) != 1 {
		t.Fatalf("not armed after the retry: %+v", f)
	}
}

func TestWillSeedsSessionsBeforeFirstBuild(t *testing.T) {
	w := &willEnv{env: newEnv(t, 1), now: time.UnixMilli(1793707200000)}
	working := sessionEv("s_aaaaaaaaaa", "07", "working").Payload.(wire.SessionEvent).Session
	s, _ := sessionOf(working)
	w.keeper = &WillKeeper{
		Hub: w.hub, Devices: w.svc.Devices(), Keys: w.svc.HostKeys, Host: "studio",
		Now: func() time.Time { return w.now },
		Sessions: func(context.Context) ([]session.Session, error) {
			return []session.Session{s}, nil
		},
	}
	w.keeper.start(context.Background())
	w.advance(willDebounce)
	if f, _ := w.frame(t); len(f.Pushes) != 1 {
		t.Fatalf("a session working through a restart did not keep the will armed: %+v", f)
	}
}

func TestWillCountsEventsBeforeRun(t *testing.T) {
	w := &willEnv{env: newEnv(t, 1), now: time.UnixMilli(1793707200000)}
	w.keeper = &WillKeeper{
		Hub: w.hub, Devices: w.svc.Devices(), Keys: w.svc.HostKeys, Host: "studio",
		Now: func() time.Time { return w.now },
	}
	w.keeper.Subscribe()
	w.hub.publish(managedEv("s_aaaaaaaaaa", "working"))
	w.keeper.start(context.Background()) // subscribing again adds no second observer
	w.advance(willDebounce)
	if f, _ := w.frame(t); len(f.Pushes) != 1 {
		t.Fatalf("status published before Run was missed: %+v", f)
	}
	if n := len(w.hub.fns); n != 1 {
		t.Fatalf("%d hub observers, want 1", n)
	}
}
