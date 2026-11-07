package hostinfo

import (
	"context"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/tailnet"
	"github.com/gettogo/togo/helper/internal/wire"
)

// maxIPv6PerInterface keeps privacy addresses, which macOS rotates, from
// crowding out the stable ones a phone is likely to reach.
const maxIPv6PerInterface = 2

var tailnetV6 = mustCIDR("fd7a:115c:a1e0::/48")

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// CollectRoutes lists the addresses a phone can reach this machine by: the
// mDNS name first, then each LAN interface's private IPv4 and routable IPv6
// addresses, then the Tailscale MagicDNS name and addresses. localName is the
// .local name or empty; ts is the Tailscale state, used only when running.
func CollectRoutes(
	ifaces func() ([]net.Interface, error),
	addrs func(net.Interface) ([]net.Addr, error),
	localName string,
	ts tailnet.Status,
) []wire.HostRoute {
	routes := []wire.HostRoute{}
	seen := map[string]bool{}
	add := func(r wire.HostRoute) {
		key := strings.ToLower(r.Host)
		if r.Host == "" || seen[key] {
			return
		}
		seen[key] = true
		routes = append(routes, r)
	}
	if localName != "" {
		add(wire.HostRoute{Kind: wire.RouteMDNS, Host: localName})
	}
	if list, err := ifaces(); err == nil {
		for _, i := range list {
			if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || skipped(i.Name) {
				continue
			}
			as, err := addrs(i)
			if err != nil {
				continue
			}
			var v6 int
			for _, a := range as {
				ip := addrIP(a)
				switch {
				case ip == nil || isTailnet(ip):
				case ip.To4() != nil:
					if ip.IsPrivate() {
						add(wire.HostRoute{Kind: wire.RouteLAN, Host: ip.To4().String(), Interface: i.Name})
					}
				case ip.IsGlobalUnicast() && v6 < maxIPv6PerInterface:
					v6++
					add(wire.HostRoute{Kind: wire.RouteLAN, Host: ip.String(), Interface: i.Name})
				}
			}
		}
	}
	if ts.Running {
		add(wire.HostRoute{Kind: wire.RouteTailscale, Host: ts.DNSName})
		for _, ip := range ts.IPs {
			add(wire.HostRoute{Kind: wire.RouteTailscale, Host: ip})
		}
	}
	return routes
}

func addrIP(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	}
	return nil
}

// isTailnet reports Tailscale's own ranges, which belong under the Tailscale
// route rather than a LAN interface: 100.64.0.0/10 and fd7a:115c:a1e0::/48.
func isTailnet(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 100 && v4[1]&0xC0 == 64
	}
	return tailnetV6.Contains(ip)
}

// systemLocalName is the machine's mDNS name, such as studio.local. On macOS
// it comes from scutil; on Linux it is the short host name when Avahi runs.
func systemLocalName(ctx context.Context, run tailnet.Runner) string {
	switch runtime.GOOS {
	case "darwin":
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out, err := run(ctx, "/usr/sbin/scutil", "--get", "LocalHostName")
		return localName(string(out), err)
	case "linux":
		for _, sock := range []string{"/run/avahi-daemon/socket", "/var/run/avahi-daemon/socket"} {
			if _, err := os.Stat(sock); err == nil {
				name, err := os.Hostname()
				short, _, _ := strings.Cut(name, ".")
				return localName(short, err)
			}
		}
	}
	return ""
}

func localName(name string, err error) string {
	name = strings.TrimSpace(name)
	if err != nil || name == "" || strings.ContainsAny(name, " \t\n/") {
		return ""
	}
	return name + ".local"
}

// SystemRoutes lists this machine's routes as host.info reports them.
func SystemRoutes(ctx context.Context) []wire.HostRoute {
	return CollectRoutes(
		net.Interfaces, func(i net.Interface) ([]net.Addr, error) { return i.Addrs() },
		systemLocalName(ctx, outputRun), SystemTailscale(ctx),
	)
}
