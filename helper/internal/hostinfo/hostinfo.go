// Package hostinfo reports the machine's network details so a phone can send
// Wake-on-LAN later.
package hostinfo

import (
	"bufio"
	"context"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/wire"
)

const pmsetPath = "/usr/bin/pmset"

// skipPrefixes name virtual, tunnel and peer-to-peer interfaces that cannot
// receive a magic packet.
var skipPrefixes = []string{
	"utun", "tailscale", "docker", "br-", "veth", "awdl", "llw", "bridge",
	"vmenet", "vnic", "vmnet", "vboxnet", "virbr", "lxc", "lxdbr", "cni",
	"flannel", "cali", "podman", "zt", "wg",
}

// Collect lists up, non-loopback interfaces that have a 6-byte hardware
// address and at least one private IPv4 address. womp reports whether the
// machine wakes for network access; nil means unknown, and an error from it
// is treated as unknown.
func Collect(
	ifaces func() ([]net.Interface, error),
	addrs func(net.Interface) ([]net.Addr, error),
	womp func() (*bool, error),
) (wire.HostInfo, error) {
	list, err := ifaces()
	if err != nil {
		return wire.HostInfo{}, err
	}
	info := wire.HostInfo{Interfaces: []wire.HostInterface{}}
	for _, i := range list {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || len(i.HardwareAddr) != 6 || skipped(i.Name) {
			continue
		}
		as, err := addrs(i)
		if err != nil {
			continue
		}
		var v4 []string
		for _, a := range as {
			if ip := privateIPv4(a); ip != "" {
				v4 = append(v4, ip)
			}
		}
		if len(v4) == 0 {
			continue
		}
		info.Interfaces = append(info.Interfaces, wire.HostInterface{
			Name: i.Name, MAC: strings.ToLower(i.HardwareAddr.String()), IPv4: v4,
		})
	}
	if womp != nil {
		if w, err := womp(); err == nil {
			info.WakeOnLAN = w
		}
	}
	return info, nil
}

func skipped(name string) bool {
	if rest, ok := strings.CutPrefix(name, "ap"); ok && rest != "" && strings.Trim(rest, "0123456789") == "" {
		return true
	}
	for _, p := range skipPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func privateIPv4(a net.Addr) string {
	var ip net.IP
	switch v := a.(type) {
	case *net.IPNet:
		ip = v.IP
	case *net.IPAddr:
		ip = v.IP
	}
	if v4 := ip.To4(); v4 != nil && v4.IsPrivate() {
		return v4.String()
	}
	return ""
}

// parseWomp reads the womp line of `pmset -g`; nil when absent or malformed.
func parseWomp(out string) *bool {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 || f[0] != "womp" {
			continue
		}
		var v bool
		switch f[1] {
		case "1":
			v = true
		case "0":
		default:
			return nil
		}
		return &v
	}
	return nil
}

// systemWomp reads the setting from pmset on macOS and reports unknown
// elsewhere.
func systemWomp() (*bool, error) {
	if runtime.GOOS != "darwin" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, pmsetPath, "-g").Output()
	if err != nil {
		return nil, err
	}
	return parseWomp(string(out)), nil
}
