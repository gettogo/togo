package doctor

import "github.com/gettogo/togo/helper/internal/awake"

var awakeModeLines = map[awake.Mode]string{
	awake.ModePlugged: "plugged: awake while sessions work, lid closed only on AC",
	awake.ModeBattery: "battery: awake while sessions work, lid closed on AC or battery",
}

// AwakeCheck reports how the machine is kept awake. live is false when no
// daemon answered, so st holds only the saved settings.
func AwakeCheck(st awake.Status, live bool) Check {
	c := Check{Name: "awake", Status: StatusOK}
	switch {
	case live && st.Reason == awake.ReasonUnsupported:
		c.Status, c.Detail = StatusNote, "keeping awake is not supported on this system"
		return c
	case st.Mode == awake.ModeOff:
		c.Status, c.Detail, c.Fix = StatusNote, "off: sleeps as usual while sessions work", "togo awake plugged"
		return c
	case live && st.Mode == awake.ModeBattery && st.NeedsAdmin:
		c.Status, c.Detail, c.Fix = StatusWarn, "battery mode is not set up; a closed lid on battery still sleeps", "togo awake battery"
		return c
	case live && st.Reason == awake.ReasonFailed:
		c.Status, c.Detail = StatusWarn, string(st.Mode)+": cannot hold the machine awake: "+st.Detail
		return c
	}
	c.Detail = awakeModeLines[st.Mode]
	switch {
	case !live:
		c.Detail += "; daemon not running"
	case st.Held:
		c.Detail += "; holding now"
	default:
		c.Detail += "; not holding now"
	}
	return c
}
