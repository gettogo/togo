// Package awake keeps the host from sleeping while a Togo session is
// working or waiting on the user, so closing a laptop lid does not stop an
// agent, and lets it sleep normally otherwise.
//
// On macOS a supervised `caffeinate -s -i -w <daemon pid>` holds the power
// assertions; battery mode also sets `pmset -a disablesleep 1` through a
// narrow sudoers rule. On Linux a supervised systemd-inhibit holds a sleep,
// idle and lid-switch block. Every OS call sits behind a small interface so
// tests use fakes.
package awake

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gettogo/togo/helper/internal/state"
)

// Mode is how far the helper goes to keep the host awake.
type Mode string

// Modes.
const (
	// ModeOff never holds anything.
	ModeOff Mode = "off"
	// ModePlugged holds a sleep assertion while sessions are active. A
	// closed lid stays awake only on AC power.
	ModePlugged Mode = "plugged"
	// ModeBattery also disables sleep while active, so a closed lid on
	// battery stays awake. It needs a one-time admin setup on macOS.
	ModeBattery Mode = "battery"
)

// ParseMode returns the Mode named s.
func ParseMode(s string) (Mode, bool) {
	switch m := Mode(s); m {
	case ModeOff, ModePlugged, ModeBattery:
		return m, true
	}
	return "", false
}

// Battery floor limits, in percent.
const (
	DefaultBatteryFloor = 20
	MinBatteryFloor     = 10
	MaxBatteryFloor     = 50
)

// Settings are the per-machine choices, kept in TOGO_HOME.
type Settings struct {
	Mode         Mode `json:"mode"`
	BatteryFloor int  `json:"batteryFloor"`
}

// DefaultSettings are used until the user changes anything.
var DefaultSettings = Settings{Mode: ModePlugged, BatteryFloor: DefaultBatteryFloor}

func (s Settings) validate() error {
	if _, ok := ParseMode(string(s.Mode)); !ok {
		return fmt.Errorf("unknown mode %q", s.Mode)
	}
	return ValidateFloor(s.BatteryFloor)
}

// ValidateFloor checks a battery floor is within 10 to 50 percent.
func ValidateFloor(n int) error {
	if n < MinBatteryFloor || n > MaxBatteryFloor {
		return fmt.Errorf("battery floor must be %d to %d", MinBatteryFloor, MaxBatteryFloor)
	}
	return nil
}

// SettingsPath is the settings file in TOGO_HOME.
func SettingsPath(home string) string { return filepath.Join(home, "awake.json") }

// OverrideMarker is the file that exists while disablesleep is set by Togo.
// Only a setting Togo made is ever cleared.
func OverrideMarker(home string) string { return filepath.Join(home, "awake-disablesleep") }

// LoadSettings reads the settings. A missing file gives the defaults; an
// unreadable or invalid one gives the defaults and an error to report.
func LoadSettings(home string) (Settings, error) {
	b, err := os.ReadFile(SettingsPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return DefaultSettings, nil
	}
	if err != nil {
		return DefaultSettings, err
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return DefaultSettings, fmt.Errorf("%s: %w", SettingsPath(home), err)
	}
	if err := s.validate(); err != nil {
		return DefaultSettings, fmt.Errorf("%s: %w", SettingsPath(home), err)
	}
	return s, nil
}

// SaveSettings writes the settings atomically.
func SaveSettings(home string, s Settings) error {
	if err := s.validate(); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return state.WriteFile(SettingsPath(home), append(b, '\n'))
}

func markOverride(home string) error {
	return state.WriteFile(OverrideMarker(home), []byte("disablesleep 1 set by togo\n"))
}

func overrideMarked(home string) bool {
	_, err := os.Stat(OverrideMarker(home))
	return err == nil
}

func unmarkOverride(home string) error {
	if err := os.Remove(OverrideMarker(home)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
