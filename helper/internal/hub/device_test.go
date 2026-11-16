package hub_test

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestForegroundDevicesNamesTheAppsInFront(t *testing.T) {
	e := newEnv(t)
	a := e.dial(t)
	a.call(wire.MethodEventsSubscribe, map[string]any{"deviceId": "dev_a"})
	ids, unnamed := e.hub.ForegroundDevices()
	if !ids["dev_a"] || len(ids) != 1 || unnamed {
		t.Fatalf("devices = %v, unnamed %v; want dev_a alone", ids, unnamed)
	}
	b := e.dial(t)
	b.subscribe(true)
	if _, unnamed := e.hub.ForegroundDevices(); !unnamed {
		t.Fatal("an app that named no device is not reported")
	}
	b.subscribe(false)
	a.call(wire.MethodEventsSubscribe, map[string]any{"deviceId": "dev_a", "foreground": false})
	if ids, unnamed := e.hub.ForegroundDevices(); len(ids) != 0 || unnamed {
		t.Fatalf("devices = %v, unnamed %v after both left; want none", ids, unnamed)
	}
	// An id no device can be paired under does not break the subscription.
	a.call(wire.MethodEventsSubscribe, map[string]any{"deviceId": "not an id"})
	if ids, unnamed := e.hub.ForegroundDevices(); len(ids) != 0 || !unnamed {
		t.Fatalf("devices = %v, unnamed %v for a bad id; want unnamed", ids, unnamed)
	}
}
