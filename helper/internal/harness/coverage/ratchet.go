package coverage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Status of a cell.
const (
	StatusCovered       = "covered"        // tape or generated evidence
	StatusNotApplicable = "not applicable" // the adapter cannot have it
	StatusMissing       = "missing"        // no evidence that satisfies it
)

// Matrix is the cells and the evidence found for them.
type Matrix struct {
	Cells    []Cell
	Evidence []Evidence
	byKey    map[string][]Evidence
}

// NewMatrix joins cells and evidence.
func NewMatrix(cells []Cell, evidence []Evidence) *Matrix {
	m := &Matrix{Cells: cells, Evidence: evidence, byKey: map[string][]Evidence{}}
	for _, e := range evidence {
		m.byKey[e.Key] = append(m.byKey[e.Key], e)
	}
	return m
}

// Status reports a cell's status.
func (m *Matrix) Status(c Cell) string {
	if c.NotApplicable != "" {
		return StatusNotApplicable
	}
	for _, e := range m.byKey[c.Key()] {
		if Satisfies(e.Tier) {
			return StatusCovered
		}
	}
	return StatusMissing
}

// Weak reports a missing cell that has unit or live evidence only.
func (m *Matrix) Weak(c Cell) bool {
	return m.Status(c) == StatusMissing && len(m.byKey[c.Key()]) > 0
}

// Missing returns the sorted keys of the cells with no satisfying evidence.
func (m *Matrix) Missing() []string {
	var out []string
	for _, c := range m.Cells {
		if m.Status(c) == StatusMissing {
			out = append(out, c.Key())
		}
	}
	sort.Strings(out)
	return out
}

// UnknownEvidence lists evidence whose key is not an applicable cell.
func (m *Matrix) UnknownEvidence() []string {
	idx := Index(m.Cells)
	var out []string
	for _, e := range m.Evidence {
		c, ok := idx[e.Key]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s:%d names %q, which is not a cell", e.File, e.Line, e.Key))
		case c.NotApplicable != "":
			out = append(out, fmt.Sprintf("%s:%d names %q, which is not applicable: %s", e.File, e.Line, e.Key, c.NotApplicable))
		}
	}
	return out
}

// CheckRatchet compares the listed keys with the matrix and returns one
// message per problem; none means the list is exactly right.
func (m *Matrix) CheckRatchet(listed []string) []string {
	idx := Index(m.Cells)
	want := map[string]bool{}
	for _, k := range m.Missing() {
		want[k] = true
	}
	have := map[string]bool{}
	var problems []string
	for _, k := range listed {
		if have[k] {
			problems = append(problems, fmt.Sprintf("missing.txt lists %s twice: remove one line", k))
		}
		have[k] = true
		c, real := idx[k]
		switch {
		case !real:
			problems = append(problems, fmt.Sprintf("missing.txt lists %s, which is not a cell (the agent or capability is gone): remove the line", k))
		case c.NotApplicable != "":
			problems = append(problems, fmt.Sprintf("missing.txt lists %s, which is not applicable (%s): remove the line", k, c.NotApplicable))
		case !want[k]:
			problems = append(problems, fmt.Sprintf("missing.txt lists %s, which now has tape or generated evidence: remove the line", k))
		}
	}
	for _, k := range m.Missing() {
		if !have[k] {
			problems = append(problems, fmt.Sprintf("%s has no tape or generated test and is not in missing.txt: add tests for it (coverage.Covers in the test); the list only shrinks", k))
		}
	}
	problems = append(problems, m.UnknownEvidence()...)
	if !sortedUnique(listed) {
		problems = append(problems, "missing.txt is not sorted: run go run ./internal/harness/coverage/cmd/coverage-report -update")
	}
	return problems
}

func sortedUnique(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] <= s[i-1] {
			return false
		}
	}
	return true
}

// ReadMissing reads a ratchet file: one key per line, blank lines ignored.
func ReadMissing(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// WriteMissing writes the keys, sorted, one per line.
func WriteMissing(path string, keys []string) error {
	keys = append([]string(nil), keys...)
	sort.Strings(keys)
	body := strings.Join(keys, "\n")
	if body != "" {
		body += "\n"
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

// MissingFile is the ratchet file of the real tree.
func MissingFile() string {
	return filepath.Join(HelperDir(), "internal", "harness", "coverage", "missing.txt")
}

// Load reads the real tree: cells, test declarations and conformance
// evidence.
func Load() (*Matrix, error) {
	cells, err := Cells()
	if err != nil {
		return nil, err
	}
	helper := HelperDir()
	ev, err := ScanEvidence(helper, filepath.Join(helper, "internal", "harness", "coverage"))
	if err != nil {
		return nil, err
	}
	nightly, err := NightlyAgents(filepath.Join(helper, ".."))
	if err != nil {
		return nil, err
	}
	return NewMatrix(cells, append(ev, ConformanceEvidence(cells, nightly)...)), nil
}
