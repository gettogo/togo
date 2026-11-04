package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/gettogo/togo/helper/internal/state"
	"github.com/gettogo/togo/helper/internal/wire"
)

// ErrUnknownDevice reports a deviceId that is not paired.
var ErrUnknownDevice = errors.New("push: unknown device")

// Device is a paired phone. Dead is set when the relay reports its APNs token
// unregistered; a dead device gets no pushes until it pairs again.
type Device struct {
	wire.Device
	Dead bool `json:"dead,omitempty"`
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

	mu sync.Mutex
}

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
	list = slices.DeleteFunc(list, func(x Device) bool { return x.DeviceID == dev.DeviceID })
	return d.write(append(list, Device{Device: dev}))
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

// MarkDead flags id after the relay answers 410 for its token. The token in
// the request is compared so a device that re-paired with a new token while
// the push was in flight stays live.
func (d *Devices) MarkDead(id, token string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	list, err := d.read()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(x Device) bool { return x.DeviceID == id && x.APNsToken == token })
	if i < 0 || list[i].Dead {
		return nil
	}
	list[i].Dead = true
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
	return state.WriteFile(filepath.Join(dir, devicesFile), append(data, '\n'))
}
