package hostinfo

import (
	"errors"
	"net"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/tailnet"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestCollectRoutesOrderAndFilters(t *testing.T) {
	up := net.FlagUp | net.FlagBroadcast
	list := []net.Interface{
		{Name: "en0", Flags: up},
		{Name: "lo0", Flags: up | net.FlagLoopback},
		{Name: "utun4", Flags: up},
		{Name: "en7", Flags: net.FlagBroadcast},
	}
	byName := map[string][]net.Addr{
		"en0": {
			ipnet("192.168.1.20/24"), ipnet("fe80::1/64"), ipnet("8.8.8.8/24"),
			ipnet("2001:db8::10/64"), ipnet("2001:db8::11/64"), ipnet("2001:db8::12/64"),
			ipnet("fd00::5/64"), ipnet("100.70.1.2/10"),
		},
		"lo0":   {ipnet("127.0.0.1/8")},
		"utun4": {ipnet("100.101.102.103/32"), ipnet("fd7a:115c:a1e0::1/128")},
		"en7":   {ipnet("10.0.0.9/8")},
	}
	ifs := func() ([]net.Interface, error) { return list, nil }
	addrs := func(i net.Interface) ([]net.Addr, error) { return byName[i.Name], nil }
	ts := tailnet.Status{Running: true, DNSName: "studio.tail1234.ts.net", IPs: []string{"100.101.102.103", "fd7a:115c:a1e0::1"}}
	got := CollectRoutes(ifs, addrs, "Studio.local", ts)
	want := []wire.HostRoute{
		{Kind: "mdns", Host: "Studio.local"},
		{Kind: "lan", Host: "192.168.1.20", Interface: "en0"},
		{Kind: "lan", Host: "2001:db8::10", Interface: "en0"},
		{Kind: "lan", Host: "2001:db8::11", Interface: "en0"},
		{Kind: "tailscale", Host: "studio.tail1234.ts.net"},
		{Kind: "tailscale", Host: "100.101.102.103"},
		{Kind: "tailscale", Host: "fd7a:115c:a1e0::1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("routes\n got %+v\nwant %+v", got, want)
	}
}

func TestCollectRoutesWithoutTailscaleOrName(t *testing.T) {
	ifs, addrs := fakeIfaces(net.Interface{Name: "en0", Flags: net.FlagUp})
	got := CollectRoutes(ifs, addrs, "", tailnet.Status{State: "Stopped", DNSName: "x.ts.net", IPs: []string{"100.1.2.3"}})
	if want := []wire.HostRoute{{Kind: "lan", Host: "192.168.1.20", Interface: "en0"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	broken := func() ([]net.Interface, error) { return nil, errors.New("no") }
	if got := CollectRoutes(broken, addrs, "", tailnet.Status{}); got == nil || len(got) != 0 {
		t.Errorf("got %#v, want empty non-nil", got)
	}
}

func TestLocalName(t *testing.T) {
	if got := localName("Studio-Mac-16\n", nil); got != "Studio-Mac-16.local" {
		t.Errorf("got %q", got)
	}
	for _, bad := range []string{"", "  ", "a b"} {
		if got := localName(bad, nil); got != "" {
			t.Errorf("%q gave %q", bad, got)
		}
	}
	if got := localName("x", errors.New("no")); got != "" {
		t.Errorf("error gave %q", got)
	}
}
