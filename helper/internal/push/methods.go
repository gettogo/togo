package push

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/rpc"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// InsecureRelayEnv, when "1", lets tests pair with an http relay on
// 127.0.0.1. Every other relay URL must be https.
const InsecureRelayEnv = "TOGO_TEST_RELAY_INSECURE"

var (
	deviceIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	tokenRe    = regexp.MustCompile(`^[0-9a-f]{64,200}$`)
)

// Service serves push.pair and push.unpair and hands keys and devices to the
// Sender and the mailbox reader.
type Service struct {
	// Home is $TOGO_HOME.
	Home string
	// Rand supplies the host secrets on first use; nil means crypto/rand.
	// Tests set it to fixed bytes.
	Rand io.Reader
	// Getenv reads InsecureRelayEnv; nil means no insecure relays.
	Getenv func(string) string
	// Now stamps pairings; nil means time.Now.
	Now func() time.Time

	once    sync.Once
	devices *Devices
	keysMu  sync.Mutex
	keys    *HostKeys
}

// HostKeys returns the host keys, creating them on first use.
func (s *Service) HostKeys() (HostKeys, error) {
	s.keysMu.Lock()
	defer s.keysMu.Unlock()
	if s.keys != nil {
		return *s.keys, nil
	}
	r := s.Rand
	if r == nil {
		r = rand.Reader
	}
	k, err := loadOrCreateHostKeys(s.Home, r)
	if err != nil {
		return HostKeys{}, err
	}
	s.keys = &k
	return k, nil
}

// Devices returns the pairing store.
func (s *Service) Devices() *Devices {
	s.once.Do(func() { s.devices = &Devices{Home: s.Home} })
	return s.devices
}

// Methods returns the push.* handlers for API.Ext.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{
		wire.MethodPushPair:   s.pair,
		wire.MethodPushUnpair: s.unpair,
	}
}

func (s *Service) pair(_ context.Context, raw json.RawMessage) (any, error) {
	var p wire.PairParams
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	if err := s.validatePair(p); err != nil {
		return nil, err
	}
	k, err := s.HostKeys()
	if err != nil {
		return nil, err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	p.RelayURL = strings.TrimRight(p.RelayURL, "/")
	if err := s.Devices().Put(wire.Device{PairParams: p, PairedAt: wire.Stamp(now())}); err != nil {
		return nil, err
	}
	return wire.PairResult{HostID: k.HostID, HostKey: k.HostKey()}, nil
}

func (s *Service) unpair(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		DeviceID string `json:"deviceId"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	if !deviceIDRe.MatchString(p.DeviceID) {
		return nil, invalid("deviceId must be 1-64 characters of A-Z a-z 0-9 _ -")
	}
	if err := s.Devices().Remove(p.DeviceID); err != nil {
		if errors.Is(err, ErrUnknownDevice) {
			return nil, &api.CodeError{Code: rpc.CodeNotFound}
		}
		return nil, err
	}
	return struct{}{}, nil
}

// validatePair checks every field against the schema. Messages name the
// field but never echo its value, since most of them are keys or tokens.
func (s *Service) validatePair(p wire.PairParams) error {
	switch {
	case !deviceIDRe.MatchString(p.DeviceID):
		return invalid("deviceId must be 1-64 characters of A-Z a-z 0-9 _ -")
	case p.Name == "" || utf8.RuneCountInString(p.Name) > wire.MaxDeviceName || !utf8.ValidString(p.Name):
		return invalid("name must be 1-64 characters")
	case !validKey(p.X25519):
		return invalid("x25519 must be a 32-byte base64url key")
	case !validKey(p.ApprovalKey):
		return invalid("approvalKey must be a 32-byte base64url key")
	case !tokenRe.MatchString(p.APNsToken):
		return invalid("apnsToken must be 64-200 lowercase hex characters")
	case p.LiveActivityToken != "" && !tokenRe.MatchString(p.LiveActivityToken):
		return invalid("liveActivityToken must be 64-200 lowercase hex characters")
	case p.LiveActivityStartToken != "" && !tokenRe.MatchString(p.LiveActivityStartToken):
		return invalid("liveActivityStartToken must be 64-200 lowercase hex characters")
	case p.Platform != "" && p.Platform != wire.PlatformIOS && p.Platform != wire.PlatformWatch:
		return invalid("platform must be ios or watchos")
	case p.Env != wire.EnvSandbox && p.Env != wire.EnvProduction:
		return invalid("env must be sandbox or production")
	case !s.validRelay(p.RelayURL):
		return invalid("relayUrl must be an https URL")
	}
	return nil
}

func validKey(s string) bool {
	var k [32]byte
	return decode32(s, &k)
}

// validRelay accepts https URLs with a host and no query, fragment or
// credentials. Plain http is allowed only for 127.0.0.1 under
// InsecureRelayEnv, so a test relay can run without certificates.
func (s *Service) validRelay(raw string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, " \t\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return u.Hostname() == "127.0.0.1" && s.Getenv != nil && s.Getenv(InsecureRelayEnv) == "1"
	}
	return false
}

func invalid(msg string) error { return &session.InvalidParamsError{Msg: msg} }
