package appinventory

import (
	"errors"
	"fmt"
	"slices"
)

const (
	ReplayUI = "replay-ui"
	LiveUI   = "live-ui"
	DeviceUI = "device-ui"
)

// Proof is an executed UI assertion tied to one required key and one
// candidate revision. Inventory, fixture existence, and test registrations
// cannot establish execution evidence.
type Proof struct {
	Key, Revision, Tier, Result string
}

// CheckRatchet requires the current debt list to match the unverified cells,
// and prevents growing it relative to the merge base. An initial baseline can
// be checked against itself once; subsequent checks need an independent base.
func CheckRatchet(expected, listed, base []string, evidence []Proof, revision string) error {
	var problems []error
	required := make(map[string]bool, len(expected))
	for _, key := range expected {
		if required[key] {
			problems = append(problems, fmt.Errorf("duplicate expected app cell %q", key))
		}
		required[key] = true
	}
	covered, failed := map[string]bool{}, map[string]bool{}
	for _, proof := range evidence {
		if revision == "" || proof.Revision != revision {
			continue
		}
		switch proof.Tier {
		case ReplayUI, LiveUI, DeviceUI:
		default:
			continue
		}
		if !required[proof.Key] {
			problems = append(problems, fmt.Errorf("UI result names unknown app cell %q", proof.Key))
		}
		if proof.Result == "passed" {
			covered[proof.Key] = true
		} else {
			failed[proof.Key] = true
		}
	}
	baseKeys := make(map[string]bool, len(base))
	for _, key := range base {
		baseKeys[key] = true
	}
	debt := make(map[string]bool, len(listed))
	if !slices.IsSorted(listed) {
		problems = append(problems, errors.New("app debt must be sorted"))
	}
	for _, key := range listed {
		switch {
		case debt[key]:
			problems = append(problems, fmt.Errorf("duplicate app debt %q", key))
		case !baseKeys[key]:
			problems = append(problems, fmt.Errorf("new app debt %q", key))
		case !required[key]:
			problems = append(problems, fmt.Errorf("retired app cell %q still listed", key))
		case covered[key] && !failed[key]:
			problems = append(problems, fmt.Errorf("covered app cell %q still listed", key))
		}
		debt[key] = true
	}
	for _, key := range expected {
		if (!covered[key] || failed[key]) && !debt[key] {
			problems = append(problems, fmt.Errorf("missing UI evidence for %q", key))
		}
	}
	return errors.Join(problems...)
}
