package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/wire"
)

// ErrUnknownDevice reports a deviceId that is not paired.
var ErrUnknownDevice = errors.New("push: unknown device")

// ErrStaleRegistration reports a registration no newer than the last one
// applied for the device.
var ErrStaleRegistration = errors.New("push: registration older than the last one applied")

// Device is a paired phone. Dead is set when the relay reports its APNs token
// unregistered; a dead device gets no pushes until it pairs again.
// RegisterTS is the ts of the last registration applied, so a replayed or
// reordered one is refused even after a restart.
type Device struct {
	wire.Device
	Dead             bool   `json:"dead,omitempty"`
	RegisterTS       int64  `json:"registerTs,omitempty"`
	ContextRequestID string `json:"contextRequestId,omitempty"`
	ContextTS        int64  `json:"contextTs,omitempty"`
}

type devicesJSON struct {
	V       int      `json:"v"`
	Devices []Device `json:"devices"`
}

// Devices is the pairing store, $TOGO_HOME/keys/devices.json (0600). Every
// change rewrites the file atomically, so a crash leaves the old list or the
// new one. It is safe for concurrent use within one process.
type Devices struct {
	Home string

	mu  sync.Mutex
	gen atomic.Uint64 // bumped on every write in this process
}

// Gen changes whenever this store writes, so a reader can tell whether a
// cached view is current without reading the file.
func (d *Devices) Gen() uint64 { return d.gen.Load() }

// List returns the paired devices in pairing order.
func (d *Devices) List() ([]Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.read()
}

// Put adds dev, replacing any device with the same id and clearing its dead
// flag, since pairing again supplies a fresh token.
func (d *Devices) Put(dev wire.Device) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	list, err := d.read()
	if err != nil {
		return err
	}
	// The registration clock survives pairing again, so a registration
	// captured before the re-pair cannot be replayed after it.
	var regTS, contextTS int64
	var contextRequestID string
	if i := slices.IndexFunc(list, func(x Device) bool { return x.DeviceID == dev.DeviceID }); i >= 0 {
		regTS = list[i].RegisterTS
		if list[i].ApprovalKey == dev.ApprovalKey && list[i].X25519 == dev.X25519 {
			contextTS = list[i].ContextTS
			if list[i].APNsToken == dev.APNsToken {
				contextRequestID = list[i].ContextRequestID
			}
		}
	}
	list = slices.DeleteFunc(list, func(x Device) bool { return x.DeviceID == dev.DeviceID })
	return d.write(append(list, Device{Device: dev, RegisterTS: regTS, ContextTS: contextTS, ContextRequestID: contextRequestID}))
}

// Remove unpairs id. It returns ErrUnknownDevice when id is not paired.
func (d *Devices) Remove(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	list, err := d.read()
	if err != nil {
		return err
	}
	n := len(list)
	list = slices.DeleteFunc(list, func(x Device) bool { return x.DeviceID == id })
	if len(list) == n {
		return ErrUnknownDevice
	}
	return d.write(list)
}

// MarkDead handles a 410 from the relay for one of id's tokens. For the
// APNs token the device is flagged dead; for a Live Activity token only that
// token is forgotten, since the activity is gone but the device is not. The
// token in the request is compared so a device that re-paired or registered
// a new token while the push was in flight stays as it is.
func (d *Devices) MarkDead(id, token string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	list, err := d.read()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(x Device) bool { return x.DeviceID == id })
	if i < 0 || token == "" {
		return nil
	}
	dev := &list[i]
	switch token {
	case dev.APNsToken:
		if dev.Dead {
			return nil
		}
		dev.Dead = true
	case dev.LiveActivityToken:
		dev.LiveActivityToken = ""
	case dev.LiveActivityStartToken:
		dev.LiveActivityStartToken = ""
	default:
		return nil
	}
	return d.write(list)
}

// UpdateTokens stores tokens and the command text choice a device
// registered through the mailbox at ts (Unix ms). An empty token leaves the
// stored one unchanged. A new Live Activity update token means an activity
// is live on the device. It returns ErrUnknownDevice when id is not paired
// and ErrStaleRegistration when ts is not newer than the last registration
// applied.
func (d *Devices) UpdateTokens(id, apnsToken, laToken, laStartToken string, command bool, ts int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	list, err := d.read()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(x Device) bool { return x.DeviceID == id })
	if i < 0 {
		return ErrUnknownDevice
	}
	dev := &list[i]
	if ts <= dev.RegisterTS {
		return ErrStaleRegistration
	}
	dev.RegisterTS = ts
	dev.LiveActivityCommand = command
	if apnsToken != "" && apnsToken != dev.APNsToken {
		dev.Dead = false // APNs refused the old token; this one is new
		dev.ContextRequestID = ""
	}
	for _, f := range []struct {
		dst *string
		v   string
	}{{&dev.APNsToken, apnsToken}, {&dev.LiveActivityToken, laToken}, {&dev.LiveActivityStartToken, laStartToken}} {
		if f.v != "" {
			*f.dst = f.v
		}
	}
	return d.write(list)
}

// EndLiveActivity forgets id's Live Activity update token after the
// activity was ended, if it is still token.
func (d *Devices) EndLiveActivity(id, token string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	list, err := d.read()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(x Device) bool { return x.DeviceID == id && x.LiveActivityToken == token })
	if i < 0 || token == "" {
		return nil
	}
	list[i].LiveActivityToken = ""
	return d.write(list)
}

func (d *Devices) read() ([]Device, error) {
	if d.Home == "" {
		return nil, errors.New("push: TOGO_HOME is not set")
	}
	raw, err := os.ReadFile(filepath.Join(d.Home, keysDir, devicesFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("devices: %w", err)
	}
	var j devicesJSON
	if err := json.Unmarshal(raw, &j); err != nil || j.V != keyFileV {
		// Unlike recent dirs, the device list is not safe to discard: doing
		// so would silently unpair every phone.
		return nil, errors.New("devices: unreadable keys/devices.json")
	}
	return j.Devices, nil
}

func (d *Devices) write(list []Device) error {
	dir, err := keysPath(d.Home)
	if err != nil {
		return err
	}
	if list == nil {
		list = []Device{}
	}
	data, err := json.MarshalIndent(devicesJSON{V: keyFileV, Devices: list}, "", "  ")
	if err != nil {
		return err
	}
	if err := state.WriteFile(filepath.Join(dir, devicesFile), append(data, '\n')); err != nil {
		return err
	}
	d.gen.Add(1)
	return nil
}
