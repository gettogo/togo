package awake

import (
	"os"
	"path/filepath"
	"testing"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseBatt(t *testing.T) {
	for _, c := range []struct {
		file     string
		source   string
		percent  int // -1 for none
		charging bool
	}{
		{"batt-ac.txt", PowerAC, 100, false},
		{"batt-battery.txt", PowerBattery, 76, false},
		{"batt-charging.txt", PowerAC, 54, true},
		{"batt-ac-not-charging.txt", PowerAC, 80, false},
		{"batt-desktop.txt", PowerAC, -1, false},
	} {
		t.Run(c.file, func(t *testing.T) {
			p, err := ParseBatt(readTestdata(t, c.file))
			if err != nil {
				t.Fatal(err)
			}
			if p.Source != c.source || p.Charging != c.charging {
				t.Errorf("got %+v, want source %s charging %v", p, c.source, c.charging)
			}
			switch {
			case c.percent < 0 && p.Percent != nil:
				t.Errorf("percent = %d, want none", *p.Percent)
			case c.percent >= 0 && (p.Percent == nil || *p.Percent != c.percent):
				t.Errorf("percent = %v, want %d", p.Percent, c.percent)
			}
		})
	}
}

func TestParseBattMalformed(t *testing.T) {
	for _, in := range []string{readTestdata(t, "batt-malformed.txt"), "", "Now drawing from 'UPS Power'\n"} {
		p, err := ParseBatt(in)
		if err == nil {
			t.Errorf("ParseBatt(%q) = %+v, want an error", in, p)
		}
		if p.Source != PowerUnknown || p.Percent != nil {
			t.Errorf("ParseBatt(%q) = %+v, want unknown power", in, p)
		}
	}
}

func TestParseBattIgnoresBadPercent(t *testing.T) {
	p, err := ParseBatt("Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t140%; discharging; present: true\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != PowerBattery || p.Percent != nil {
		t.Errorf("got %+v, want battery without a percent", p)
	}
}

func TestParseTherm(t *testing.T) {
	for file, want := range map[string]bool{
		"therm-ok.txt":      false,
		"therm-limited.txt": true,
		"therm-warning.txt": true,
	} {
		if got := ParseTherm(readTestdata(t, file)); got != want {
			t.Errorf("%s: ParseTherm = %v, want %v", file, got, want)
		}
	}
	if ParseTherm("Thermal warning level set to 0.\n") {
		t.Error("level 0 is the all-clear, not a warning")
	}
	if ParseTherm("\tCPU_Speed_Limit \t= 100\n") {
		t.Error("a full speed limit is not a warning")
	}
}
