package doctor

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/gettogo/togo/helper/internal/tailnet"
	"github.com/gettogo/togo/helper/internal/wire"
)

const tailscaleDownload = "https://tailscale.com/download"

// checkTailscale reports whether Tailscale can carry Togo away from home:
// installed, connected and under which MagicDNS name. A second line warns
// when Tailscale SSH is on, since it can stall key logins.
func checkTailscale(ctx context.Context, e Env) []Check {
	path, _, ok := find(e, "tailscale")
	if !ok && e.TailscaleApp != "" && executableFile(e.TailscaleApp) {
		path, ok = e.TailscaleApp, true
	}
	if !ok {
		return []Check{{
			Name: "tailscale", Status: StatusNote,
			Detail: "not installed; install it to reach this machine away from home",
			Fix:    tailscaleDownload,
		}}
	}
	c := Check{Name: "tailscale", Path: path}
	out, _ := e.Run(ctx, path, "status", "--json", "--peers=false")
	st, err := tailnet.Parse([]byte(jsonPart(out)))
	switch {
	case err != nil:
		c.Status, c.Detail, c.Fix = StatusNote, "installed but not running", "open Tailscale and sign in"
	case !st.Running:
		c.Status, c.Detail, c.Fix = StatusNote, "installed but not connected ("+st.State+")", "tailscale up"
	case st.DNSName == "":
		c.Status, c.Detail = StatusOK, "connected at "+strings.Join(st.IPs, ", ")+"; turn on MagicDNS for a name that never changes"
	default:
		c.Status, c.Detail = StatusOK, "connected as "+st.DNSName
		if len(st.IPs) > 0 {
			c.Detail += " (" + st.IPs[0] + ")"
		}
	}
	checks := []Check{c}
	if ssh, ok := checkTailscaleSSH(ctx, e, path); ok {
		checks = append(checks, ssh)
	}
	return checks
}

// jsonPart drops anything Run captured before the JSON, such as a warning
// on stderr.
func jsonPart(out string) string {
	if i := strings.Index(out, "{"); i > 0 {
		return out[i:]
	}
	return out
}

func checkTailscaleSSH(ctx context.Context, e Env, path string) (Check, bool) {
	out, err := e.Run(ctx, path, "debug", "prefs")
	if err != nil {
		return Check{}, false
	}
	var prefs struct {
		RunSSH bool
	}
	if json.Unmarshal([]byte(jsonPart(out)), &prefs) != nil || !prefs.RunSSH {
		return Check{}, false
	}
	return Check{
		Name:   "tailscale ssh",
		Status: StatusNote,
		Detail: "Tailscale SSH is on and can stall key logins; run tailscale set --ssh=false to use this machine's sshd",
		Path:   path,
	}, true
}

// checkRoutes lists the addresses the phone races when it connects.
func checkRoutes(routes []wire.HostRoute) Check {
	if len(routes) == 0 {
		return Check{Name: "routes", Status: StatusWarn, Detail: "no network address found; the phone can only use the address you typed"}
	}
	hosts := make([]string, 0, len(routes))
	for _, r := range routes {
		hosts = append(hosts, r.Host)
	}
	return Check{Name: "routes", Status: StatusOK, Detail: strings.Join(hosts, ", ")}
}

func executableFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
