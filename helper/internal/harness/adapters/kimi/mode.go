package kimi

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// initialMode is the mode Kimi starts in before its wire log says
// anything: Kimi 2.5 writes no permission.set_mode record for the default
// (ask) mode, so without this the chip read "Mode" until the first change.
// The launch flags win; then default_plan_mode in config.toml; then Kimi's
// documented default, ask ("default").
func initialMode(argv []string, planByDefault bool) string {
	mode := ""
	for _, a := range argv {
		switch a {
		case "--yolo":
			mode = "yolo"
		case "--auto":
			mode = "auto"
		case "--plan":
			return "plan"
		}
	}
	if planByDefault {
		return "plan"
	}
	if mode != "" {
		return mode
	}
	return "default"
}

// configPath is Kimi's config file; a variable for tests.
var configPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kimi-code", "config.toml")
}

// defaultPlanMode reads the top-level default_plan_mode key of Kimi's
// config without parsing the rest (which also holds provider keys).
func defaultPlanMode() bool {
	p := configPath()
	if p == "" {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "[") {
			return false // past the top-level table
		}
		k, v, ok := strings.Cut(l, "=")
		if ok && strings.TrimSpace(k) == "default_plan_mode" {
			return strings.TrimSpace(strings.SplitN(v, "#", 2)[0]) == "true"
		}
	}
	return false
}
