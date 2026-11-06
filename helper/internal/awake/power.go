package awake

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Power sources in Status.Power.
const (
	PowerAC      = "ac"
	PowerBattery = "battery"
	PowerUnknown = "unknown"
)

// Power is what the host reports about its power supply.
type Power struct {
	Source string
	// Percent is the battery charge, nil without a battery.
	Percent  *int
	Charging bool
	// Thermal is true while the system reports a thermal warning or a CPU
	// speed limit.
	Thermal bool
}

// PowerReader reads the host's power state.
type PowerReader interface {
	Read(ctx context.Context) (Power, error)
}

var (
	drawingRE = regexp.MustCompile(`Now drawing from '([^']+)'`)
	percentRE = regexp.MustCompile(`\t(\d{1,3})%;\s*([^;]*)`)
	speedRE   = regexp.MustCompile(`CPU_Speed_Limit\s*=\s*(\d+)`)
	levelRE   = regexp.MustCompile(`thermal warning level[^0-9]*(\d+)`)
)

// ParseBatt reads the output of `pmset -g batt`. A Mac without a battery
// reports only its source. Output it does not recognise is an error with
// unknown power.
func ParseBatt(out string) (Power, error) {
	unknown := Power{Source: PowerUnknown}
	m := drawingRE.FindStringSubmatch(out)
	if m == nil {
		return unknown, errors.New("pmset -g batt: no power source line")
	}
	var p Power
	switch m[1] {
	case "AC Power":
		p.Source = PowerAC
	case "Battery Power":
		p.Source = PowerBattery
	default:
		return unknown, errors.New("pmset -g batt: unknown power source")
	}
	if b := percentRE.FindStringSubmatch(out); b != nil {
		if n, err := strconv.Atoi(b[1]); err == nil && n <= 100 {
			p.Percent = &n
		}
		p.Charging = strings.TrimSpace(b[2]) == "charging"
	}
	return p, nil
}

// ParseTherm reads the output of `pmset -g therm`: a recorded thermal
// warning level or a CPU speed limit below 100 is a warning.
func ParseTherm(out string) bool {
	for line := range strings.SplitSeq(out, "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if !strings.Contains(l, "thermal warning level") || strings.HasPrefix(l, "note: no thermal warning") {
			continue
		}
		// "Thermal warning level set to 0." is the all-clear.
		if m := levelRE.FindStringSubmatch(l); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n == 0 {
				continue
			}
		}
		return true
	}
	for _, m := range speedRE.FindAllStringSubmatch(out, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n < 100 {
			return true
		}
	}
	return false
}

// Pmset reads power state from /usr/bin/pmset.
type Pmset struct {
	// Output runs argv and returns its standard output. Nil runs it.
	Output func(ctx context.Context, argv ...string) (string, error)
}

const pmsetPath = "/usr/bin/pmset"

func (p Pmset) output(ctx context.Context, argv ...string) (string, error) {
	if p.Output != nil {
		return p.Output(ctx, argv...)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	return string(out), err
}

// Read implements PowerReader. A failed thermal read counts as no warning.
func (p Pmset) Read(ctx context.Context) (Power, error) {
	out, err := p.output(ctx, pmsetPath, "-g", "batt")
	if err != nil {
		return Power{Source: PowerUnknown}, err
	}
	pw, err := ParseBatt(out)
	if err != nil {
		return pw, err
	}
	if therm, err := p.output(ctx, pmsetPath, "-g", "therm"); err == nil {
		pw.Thermal = ParseTherm(therm)
	}
	return pw, nil
}
