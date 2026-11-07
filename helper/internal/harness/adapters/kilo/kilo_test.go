package kilo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/opencode"
	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/source/httpsrc"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

const recordedSession = "ses_e4bf011f2ffeibDoj0CVGRD9Pl"

func parse(t *testing.T) *spec.Adapter {
	t.Helper()
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdapterFileParses(t *testing.T) {
	a := parse(t)
	if a.Adapter != "kilo" || a.Endpoint == nil || a.Endpoint.Kind != spec.EndpointHTTP {
		t.Fatalf("adapter %q endpoint %+v", a.Adapter, a.Endpoint)
	}
	if got := a.Feeds["events"].Mapper; got != Mapper {
		t.Fatalf("events mapper %q", got)
	}
	if len(a.Modes.Choices) != 5 || a.Modes.Choices[0].ID != "code" {
		t.Fatalf("modes %+v", a.Modes.Choices)
	}
}

// The engine accepts the file: the mapper and runners exist, jq compiles.
func TestEngineLoadsAdapter(t *testing.T) {
	var logs []string
	engine.New(engine.Config{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	for _, l := range logs {
		if strings.Contains(l, "kilo") {
			t.Fatal(l)
		}
	}
}

// Kilo serves OpenCode's API: every source and action is OpenCode's, so a
// fix to one file is a fix to both.
func TestSameRoutesAsOpenCode(t *testing.T) {
	a := parse(t)
	o, err := spec.Parse(opencode.AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Sources, o.Sources) {
		t.Error("sources differ from OpenCode's")
	}
	if !reflect.DeepEqual(a.Actions, o.Actions) {
		t.Error("actions differ from OpenCode's")
	}
	if !reflect.DeepEqual(a.Screens, o.Screens) || !reflect.DeepEqual(a.Mission, o.Mission) {
		t.Error("screens or mission differ from OpenCode's")
	}
}

// A session created on Kilo 7.8.27 maps with the OpenCode mapper: the
// session becomes the followed one and the agent reads idle.
func TestRecordedEvents(t *testing.T) {
	b, err := os.ReadFile("testdata/events.sse")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := harness.LookupMapper(Mapper)
	if !ok {
		t.Fatal("mapper not registered")
	}
	m := f(harness.Env{Cwd: "/Users/user/proj"})
	p := httpsrc.NewParser(bytes.NewReader(b))
	t0 := time.Date(2026, 11, 7, 3, 31, 0, 0, time.UTC)
	n := 0
	for ; ; n++ {
		ev, err := p.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		m.Map(harness.Record{Source: "events", At: t0.Add(time.Duration(n) * 50 * time.Millisecond), Data: json.RawMessage(ev.Data)})
	}
	if n < 5 {
		t.Fatalf("only %d events", n)
	}
	st := m.State()
	if st["harness_session"] != recordedSession || st["working"] != "false" {
		t.Fatalf("state %v", st)
	}
}

// Kilo's /config/providers carries keys from the environment in plain text,
// as OpenCode's does; neither screen source passes one on.
func TestProvidersCarryNoKey(t *testing.T) {
	raw, err := os.ReadFile("testdata/providers.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(raw) }))
	defer srv.Close()
	a := parse(t)
	for _, name := range []string{"models", "providers"} {
		block, _ := json.Marshal(a.Sources[name].HTTP)
		out, err := httpsrc.HTTP{}.Fetch(context.Background(), harness.SourceSpec{Name: name, Kind: spec.KindHTTP, Raw: block}, harness.Env{Endpoint: srv.URL})
		if err != nil {
			t.Fatal(err)
		}
		for _, leak := range []string{"sk-", "Bearer"} {
			if strings.Contains(string(out), leak) {
				t.Fatalf("%s: %q reached the screen pipeline: %s", name, leak, out)
			}
		}
	}
}
