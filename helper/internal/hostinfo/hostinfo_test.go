package hostinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
)

const fixturesDir = "../../../protocol/v1/fixtures/pending"

func mac(s string) net.HardwareAddr {
	m, err := net.ParseMAC(s)
	if err != nil {
		panic(err)
	}
	return m
}

func ipnet(s string) net.Addr {
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	n.IP = ip
	return n
}

func fakeIfaces(list ...net.Interface) (func() ([]net.Interface, error), func(net.Interface) ([]net.Addr, error)) {
	addrs := map[string][]net.Addr{}
	for _, i := range list {
		addrs[i.Name] = []net.Addr{ipnet("192.168.1.20/24"), ipnet("fe80::1/64")}
	}
	return func() ([]net.Interface, error) { return list, nil },
		func(i net.Interface) ([]net.Addr, error) { return addrs[i.Name], nil }
}

func noWomp() (*bool, error) { return nil, nil }

func TestCollectFiltersVirtual(t *testing.T) {
	up := net.FlagUp | net.FlagBroadcast
	hw := mac("02:00:00:00:00:01")
	list := []net.Interface{
		{Name: "en0", Flags: up, HardwareAddr: hw},
		{Name: "lo0", Flags: up | net.FlagLoopback, HardwareAddr: hw},
		{Name: "en5", Flags: net.FlagBroadcast, HardwareAddr: hw},
		{Name: "en6", Flags: up},
		{Name: "utun3", Flags: up, HardwareAddr: hw},
		{Name: "tailscale0", Flags: up, HardwareAddr: hw},
		{Name: "docker0", Flags: up, HardwareAddr: hw},
		{Name: "br-1234", Flags: up, HardwareAddr: hw},
		{Name: "veth9", Flags: up, HardwareAddr: hw},
		{Name: "awdl0", Flags: up, HardwareAddr: hw},
		{Name: "llw0", Flags: up, HardwareAddr: hw},
		{Name: "bridge0", Flags: up, HardwareAddr: hw},
		{Name: "ap1", Flags: up, HardwareAddr: hw},
		{Name: "eth0", Flags: up, HardwareAddr: mac("02:00:00:00:00:02")},
	}
	ifs, addrs := fakeIfaces(list...)
	got, err := Collect(ifs, addrs, noWomp)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, i := range got.Interfaces {
		names = append(names, i.Name)
	}
	if want := []string{"en0", "eth0"}; !reflect.DeepEqual(names, want) {
		t.Errorf("interfaces = %v, want %v", names, want)
	}
	if got.WakeOnLAN != nil {
		t.Errorf("wakeOnLan = %v, want nil", *got.WakeOnLAN)
	}
}

func TestCollectKeepsOnlyPrivateIPv4(t *testing.T) {
	i := net.Interface{Name: "en0", Flags: net.FlagUp, HardwareAddr: mac("02:00:00:00:00:01")}
	ifs := func() ([]net.Interface, error) { return []net.Interface{i}, nil }
	addrs := func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{ipnet("8.8.8.8/24"), ipnet("10.0.0.5/8"), &net.IPAddr{IP: net.ParseIP("172.16.0.2")}, ipnet("169.254.1.1/16")}, nil
	}
	got, err := Collect(ifs, addrs, noWomp)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"10.0.0.5", "172.16.0.2"}; len(got.Interfaces) != 1 || !reflect.DeepEqual(got.Interfaces[0].IPv4, want) {
		t.Errorf("got %+v, want ipv4 %v", got.Interfaces, want)
	}
	noPrivate := func(net.Interface) ([]net.Addr, error) { return []net.Addr{ipnet("8.8.8.8/24")}, nil }
	got, _ = Collect(ifs, noPrivate, noWomp)
	if len(got.Interfaces) != 0 {
		t.Errorf("interface without private IPv4 kept: %+v", got.Interfaces)
	}
}

func TestCollectFormatsMAC(t *testing.T) {
	ifs, addrs := fakeIfaces(net.Interface{Name: "en0", Flags: net.FlagUp, HardwareAddr: mac("AA:BB:0C:0D:0E:0F")})
	on := true
	got, err := Collect(ifs, addrs, func() (*bool, error) { return &on, nil })
	if err != nil {
		t.Fatal(err)
	}
	if got.Interfaces[0].MAC != "aa:bb:0c:0d:0e:0f" {
		t.Errorf("mac = %q", got.Interfaces[0].MAC)
	}
	if got.WakeOnLAN == nil || !*got.WakeOnLAN {
		t.Errorf("wakeOnLan = %v", got.WakeOnLAN)
	}
	if !reflect.DeepEqual(got.Interfaces[0].IPv4, []string{"192.168.1.20"}) {
		t.Errorf("ipv4 = %v", got.Interfaces[0].IPv4)
	}
}

func TestWompParse(t *testing.T) {
	for _, tc := range []struct {
		file string
		want *bool
	}{
		{"pmset-g-womp-on.txt", ptr(true)},
		{"pmset-g-womp-off.txt", ptr(false)},
		{"pmset-g-womp-absent.txt", nil},
	} {
		raw, err := os.ReadFile(filepath.Join("testdata", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		got := parseWomp(string(raw))
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("%s: got %v, want %v", tc.file, got, tc.want)
		}
	}
	if parseWomp("womp banana\n") != nil || parseWomp("") != nil {
		t.Error("malformed womp parsed")
	}
}

func ptr(b bool) *bool { return &b }

func TestFixtures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixturesDir, "host.info.json"))
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
	ifs, addrs := fakeIfaces(net.Interface{Name: "en0", Flags: net.FlagUp, HardwareAddr: mac("02:00:00:00:00:01")})
	s := &Service{Interfaces: ifs, Addrs: addrs, Womp: func() (*bool, error) { return ptr(true), nil }}
	a := &api.API{Ext: s.Methods()}
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
	if !reflect.DeepEqual(got, want) {
		t.Errorf("response %s\nwant %s", last, fx.Response)
	}
}

func TestRejectsParams(t *testing.T) {
	s := &Service{Interfaces: func() ([]net.Interface, error) { return nil, nil }}
	if _, err := s.Methods()[Method](context.Background(), json.RawMessage(`[1]`)); err == nil {
		t.Error("params accepted")
	}
}

func TestSkipped(t *testing.T) {
	for _, n := range []string{"utun3", "tailscale0", "docker0", "br-1a", "veth9", "awdl0", "llw0", "bridge0",
		"vmenet0", "vnic0", "vmnet8", "vboxnet0", "virbr0", "lxc0", "lxdbr0", "cni0", "flannel.1", "cali123",
		"podman0", "zt0abc", "wg0", "ap1", "ap0"} {
		if !skipped(n) {
			t.Errorf("%s kept", n)
		}
	}
	for _, n := range []string{"en0", "eth0", "enp3s0", "wlan0", "apple0", "ap", "apt0", "wlp2s0"} {
		if skipped(n) {
			t.Errorf("%s skipped", n)
		}
	}
}
