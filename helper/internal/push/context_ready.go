package push

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
)

// ContextReady records the exact request a device saved for its renderer.
// A separate timestamp prevents an old receipt from replacing a newer one;
// token registrations and answers have independent ordering.
func (d *Devices) ContextReady(id, requestID, tokenHash string, ts int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	list, err := d.read()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(x Device) bool { return x.DeviceID == id && !x.Dead })
	if i < 0 {
		return ErrUnknownDevice
	}
	sum := sha256.Sum256([]byte(list[i].APNsToken))
	if tokenHash != hex.EncodeToString(sum[:]) {
		return errors.New("push: request receipt has an obsolete device token")
	}
	if ts <= list[i].ContextTS {
		return ErrStaleRegistration
	}
	list[i].ContextRequestID, list[i].ContextTS = requestID, ts
	return d.write(list)
}
