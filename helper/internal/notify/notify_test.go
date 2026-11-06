package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
)

const fixturesDir = "../../../protocol/v1/fixtures"

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Path: filepath.Join(t.TempDir(), "notify.json")}
}

func ptr[T any](v T) *T { return &v }

func TestDefaultsWhenMissing(t *testing.T) {
	if got := newStore(t).Get(); got != Defaults {
		t.Fatalf("got %+v", got)
	}
}

func TestUnreadableFileGivesDefaultsAndLogsOnce(t *testing.T) {
	s := newStore(t)
	logs := 0
	s.Logf = func(string, ...any) { logs++ }
	if err := os.WriteFile(s.Path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.Get() != Defaults || s.Get() != Defaults || logs != 1 {
		t.Fatalf("logs = %d", logs)
	}
}

// A bad value costs only its own field: a file that says muted keeps the
// machine muted when another field is out of range or mistyped.
func TestBadFieldKeepsTheOthers(t *testing.T) {
	s := newStore(t)
	logs := 0
	s.Logf = func(string, ...any) { logs++ }
	if err := os.WriteFile(s.Path, []byte(`{"idleSeconds":1,"muted":true,"remindMinutes":"often"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	want := Settings{IdleSeconds: Defaults.IdleSeconds, Muted: true, RemindMinutes: Defaults.RemindMinutes}
	if got := s.Get(); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if logs != 1 {
		t.Fatalf("logs = %d, want 1", logs)
	}
}

// Get is called every second, so it reads the file again only when the
// file's size or modification time changed.
func TestGetRereadsOnlyAChangedFile(t *testing.T) {
	s := newStore(t)
	write := func(body string, mod time.Time) {
		t.Helper()
		if err := os.WriteFile(s.Path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(s.Path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	then := time.Now().Add(-time.Hour).Truncate(time.Second)
	write(`{"idleSeconds":7,"muted":false,"remindMinutes":10}`, then)
	if got := s.Get(); got.IdleSeconds != 7 {
		t.Fatalf("got %+v", got)
	}
	write(`{"idleSeconds":8,"muted":false,"remindMinutes":10}`, then)
	if got := s.Get(); got.IdleSeconds != 7 {
		t.Fatalf("an unchanged size and time read the file again: %+v", got)
	}
	write(`{"idleSeconds":8,"muted":false,"remindMinutes":10}`, then.Add(time.Second))
	if got := s.Get(); got.IdleSeconds != 8 {
		t.Fatalf("a changed file was not read: %+v", got)
	}
}

func TestSetMethodReturnsAndKeepsSettings(t *testing.T) {
	s := newStore(t)
	res, err := s.Methods()[MethodSet](context.Background(), json.RawMessage(`{"muted":true,"remindMinutes":0}`))
	want := Settings{IdleSeconds: Defaults.IdleSeconds, Muted: true, RemindMinutes: 0}
	if err != nil || res != want {
		t.Fatalf("notify.set = %+v, %v; want %+v", res, err, want)
	}
	res, err = s.Methods()[MethodGet](context.Background(), json.RawMessage(`{}`))
	if err != nil || res != want {
		t.Fatalf("notify.get after set = %+v, %v; want %+v", res, err, want)
	}
	if got := (&Store{Path: s.Path}).Get(); got != want {
		t.Fatalf("a new store reads %+v; want %+v", got, want)
	}
}

func TestSetValidatesRanges(t *testing.T) {
	s := newStore(t)
	for _, p := range []Patch{
		{IdleSeconds: ptr(2)}, {IdleSeconds: ptr(601)},
		{RemindMinutes: ptr(1)}, {RemindMinutes: ptr(121)},
	} {
		_, err := s.Set(p)
		var ip *session.InvalidParamsError
		if !errors.As(err, &ip) {
			t.Errorf("%+v: err = %v", p, err)
		}
	}
	if _, err := os.Stat(s.Path); err == nil {
		t.Error("rejected set wrote a file")
	}
	got, err := s.Set(Patch{RemindMinutes: ptr(0), IdleSeconds: ptr(3)})
	if err != nil || got.RemindMinutes != 0 || got.IdleSeconds != 3 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSetPersists0600Atomic(t *testing.T) {
	s := newStore(t)
	if _, err := s.Set(Patch{Muted: ptr(true), IdleSeconds: ptr(15)}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.Path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("stat %v, %v", fi, err)
	}
	ents, _ := os.ReadDir(filepath.Dir(s.Path))
	if len(ents) != 1 {
		t.Fatalf("leftover files: %v", ents)
	}
	again := &Store{Path: s.Path}
	if got := again.Get(); got != (Settings{IdleSeconds: 15, Muted: true, RemindMinutes: 10}) {
		t.Fatalf("got %+v", got)
	}
}

func TestSetCallsOnChange(t *testing.T) {
	s := newStore(t)
	var seen []Settings
	s.OnChange = func(v Settings) { seen = append(seen, v) }
	_, _ = s.Set(Patch{IdleSeconds: ptr(1)})
	_, _ = s.Set(Patch{Muted: ptr(true)})
	if len(seen) != 1 || !seen[0].Muted {
		t.Fatalf("seen = %+v", seen)
	}
}

func TestSetNeedsAField(t *testing.T) {
	_, err := newStore(t).Methods()[MethodSet](context.Background(), json.RawMessage(`{}`))
	var ip *session.InvalidParamsError
	if !errors.As(err, &ip) {
		t.Fatalf("err = %v", err)
	}
}

func TestFixtures(t *testing.T) {
	for _, name := range []string{"notify.get.json", "notify.set.json", "notify.set-bad.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(fixturesDir, name))
			if err != nil {
				t.Fatal(err)
			}
			var fx struct {
				Setup    []json.RawMessage `json:"setup"`
				Request  json.RawMessage   `json:"request"`
				Response json.RawMessage   `json:"response"`
			}
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			a := &api.API{Ext: newStore(t).Methods()}
			var in bytes.Buffer
			for _, req := range append(append([]json.RawMessage{}, fx.Setup...), fx.Request) {
				if err := rpc.WriteFrame(&in, req); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			if err := rpc.Serve(context.Background(), &in, &out, a, rpc.HelperInfo{Version: "0.0.0-test"}); err != nil {
				t.Fatal(err)
			}
			var last json.RawMessage
			for out.Len() > 0 {
				if last, err = rpc.ReadFrame(&out); err != nil {
					t.Fatal(err)
				}
			}
			var got, want any
			_ = json.Unmarshal(last, &got)
			_ = json.Unmarshal(fx.Response, &want)
			if name == "notify.set-bad.json" {
				// The reason text is free-form.
				got.(map[string]any)["error"].(map[string]any)["data"].(map[string]any)["reason"] = "<any>"
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("response %s\nwant %s", last, fx.Response)
			}
		})
	}
}
