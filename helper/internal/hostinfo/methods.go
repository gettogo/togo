package hostinfo

import (
	"context"
	"encoding/json"
	"net"

	"github.com/gettogo/togo/helper/internal/api"
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
}

// Methods returns host.info for api.API.Ext.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{Method: s.info}
}

func (s *Service) info(_ context.Context, raw json.RawMessage) (any, error) {
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
	return Collect(ifaces, addrs, womp)
}
