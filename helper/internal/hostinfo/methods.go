package hostinfo

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"

	"github.com/gettogo/togo/helper/internal/agents"
	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/tailnet"
)

// Protocol names.
const (
	Capability = "hostinfo.v1"
	Method     = "host.info"
)

// Service answers host.info. Nil fields use the real system.
type Service struct {
	Interfaces func() ([]net.Interface, error)
	Addrs      func(net.Interface) ([]net.Addr, error)
	Womp       func() (*bool, error)
	// LocalName returns the mDNS name, such as studio.local, or empty.
	LocalName func(ctx context.Context) string
	// Tailscale returns the Tailscale state; a zero Status when absent.
	Tailscale func(ctx context.Context) tailnet.Status
}

// Methods returns host.info for api.API.Ext.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{Method: s.info}
}

func (s *Service) info(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct{}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	ifaces, addrs, womp := s.Interfaces, s.Addrs, s.Womp
	if ifaces == nil {
		ifaces = net.Interfaces
	}
	if addrs == nil {
		addrs = func(i net.Interface) ([]net.Addr, error) { return i.Addrs() }
	}
	if womp == nil {
		womp = systemWomp
	}
	localName, ts := s.LocalName, s.Tailscale
	if localName == nil {
		localName = func(ctx context.Context) string { return systemLocalName(ctx, outputRun) }
	}
	if ts == nil {
		ts = SystemTailscale
	}
	info, err := Collect(ifaces, addrs, womp)
	if err != nil {
		return nil, err
	}
	info.Routes = CollectRoutes(ifaces, addrs, localName(ctx), ts(ctx))
	return info, nil
}

// SystemTailscale finds the tailscale tool on the login PATH, in the usual
// install folders or in the app bundle, and asks it for this machine's state.
// Not installed, not running or not answering all give a zero Status.
func SystemTailscale(ctx context.Context) tailnet.Status {
	home, _ := os.UserHomeDir()
	bin := tailnet.Locate(state.LoginPath(ctx), agents.FallbackDirs(home))
	if bin == "" {
		return tailnet.Status{}
	}
	st, err := tailnet.Query(ctx, bin, outputRun)
	if err != nil {
		return tailnet.Status{}
	}
	return st
}

func outputRun(ctx context.Context, bin string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, bin, args...).Output()
}
