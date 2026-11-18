package push

import "github.com/gettogo/togo/helper/internal/wire"

// front reports who has the app on screen. all means a push is unnecessary
// for every device: an app that did not name its device is in front, or
// every paired phone and tablet is. Otherwise skip names the paired devices
// in front, which get the events directly; the others still get the push.
// An app in front that named a device which is not paired silences nobody
// else: it can only speak for itself.
//
// A Watch never counts on its own: with every phone and tablet in front it
// stays quiet as they do, and with one of them away it gets the push.
func (s *Sender) front() (all bool, skip map[string]bool) {
	if s.Hub.ForegroundSubscribers() == 0 {
		return false, nil
	}
	ids, unnamed := s.Hub.ForegroundDevices()
	if unnamed || len(ids) == 0 {
		return true, nil
	}
	devices, err := s.Devices.List()
	if err != nil {
		return true, nil
	}
	skip = map[string]bool{}
	away := 0
	for _, d := range devices {
		if d.Dead || d.EffectivePlatform() == wire.PlatformWatch {
			continue
		}
		if ids[d.DeviceID] {
			skip[d.DeviceID] = true
		} else {
			away++
		}
	}
	if away == 0 {
		// Nobody else to tell: as before, the app on screen is where the
		// user is looking, and a Watch stays quiet with it.
		return true, nil
	}
	// With no paired device in front (the app in front has pushes off, so
	// it never paired), skip is empty and every paired device is told.
	return false, skip
}
