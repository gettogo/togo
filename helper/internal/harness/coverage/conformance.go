package coverage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// conformanceCapabilities maps a capability to the scenario of
// cmd/togo-conformance that drives it on a real agent. The nightly run
// records live evidence for every agent it installs; a scenario that the
// adapter does not declare the capability for is skipped by the run, so
// only declared capabilities get a cell here.
var conformanceCapabilities = map[string]string{
	"chat":            "first_turn",   // a prompt typed from the phone is answered in the chat
	"interrupt":       "interrupt",    // harness.interrupt stops a running turn
	"needs.approvals": "approval",     // an approval reaches Needs you and is answered by id
	"needs.questions": "question",     // a question reaches Needs you and is answered by id
	"model":           "model_switch", // the model picker changes the model
	"mode":            "mode",         // the mode picker changes the mode
	"effort":          "effort",       // the effort setting changes
	"commands":        "slash",        // a slash command is submitted from the phone
	"resume":          "restore",      // a session restored after its tmux server died continues
}

// nightlyScript lists the agents the nightly run installs.
const nightlyScript = "scripts/ci/install-agents.sh"

// NightlyAgents reads the agent ids the nightly run installs from its
// install script, so the list is not kept twice. root is the repository
// root.
func NightlyAgents(root string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(root, nightlyScript))
	if err != nil {
		return nil, fmt.Errorf("coverage: %w", err)
	}
	var ids []string
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "table='"):
			in = true
		case in && strings.HasPrefix(line, "'"):
			in = false
		case in && strings.Count(line, "|") == 3:
			ids = append(ids, strings.SplitN(line, "|", 2)[0])
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("coverage: no agents found in %s", nightlyScript)
	}
	sort.Strings(ids)
	return ids, nil
}

// ConformanceEvidence returns the live evidence the conformance scenarios
// give for the agents in nightly.
func ConformanceEvidence(cells []Cell, nightly []string) []Evidence {
	run := map[string]bool{}
	for _, a := range nightly {
		run[a] = true
	}
	var out []Evidence
	for _, c := range cells {
		scenario, ok := conformanceCapabilities[c.Name]
		if c.Kind != KindCapability || !ok || !run[c.Agent] || c.NotApplicable != "" {
			continue
		}
		out = append(out, Evidence{Key: c.Key(), Tier: TierLive, File: "cmd/togo-conformance (scenario " + scenario + ")"})
	}
	return out
}
