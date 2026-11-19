package tape

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Manifest is the file next to a tape. The app reads the five string
// keys; Normalized lists the fields the exporter rewrote, so a reader
// knows which values are stand-ins.
type Manifest struct {
	ScenarioID     string   `json:"scenarioId"`
	Agent          string   `json:"agent"`
	Adapter        string   `json:"adapter"`
	SessionID      string   `json:"sessionId"`
	HelperRevision string   `json:"helperRevision"`
	Normalized     []string `json:"normalized"`
}

// Tape is one exported scenario run: the cleaned lines and what the
// manifest says. HelperRevision is set when it is written.
type Tape struct {
	Lines    [][]byte
	Manifest Manifest
}

// Bytes is the tape's file: one JSON object per line.
func (t *Tape) Bytes() []byte {
	return append(bytes.Join(t.Lines, []byte("\n")), '\n')
}

// Export runs the scenario for the agent and returns its cleaned tape. It
// returns a Skip when the scenario does not apply to the agent.
func Export(t testing.TB, sc Scenario, agent string) (*Tape, error) {
	t.Helper()
	if why := sc.Applies(agent); why != "" {
		return nil, Skip(why)
	}
	begun := time.Now()
	w, err := sc.Run(t, agent)
	if err != nil {
		return nil, err
	}
	c, err := HostCleaner(w.Scratch())
	if err != nil {
		return nil, err
	}
	if api := w.Rig.API(); api != nil && api.Port() != 0 {
		c.Ports = append(c.Ports, api.Port())
	}
	c.From, c.To = begun.Add(-time.Minute), time.Now().Add(time.Minute)
	lines, norm, err := c.Clean(w.Rec.Lines())
	if err != nil {
		return nil, fmt.Errorf("%s/%s: %w", agent, sc.ID, err)
	}
	return &Tape{Lines: lines, Manifest: Manifest{ScenarioID: sc.ID, Agent: agent, Adapter: w.Rec.Adapter,
		SessionID: w.Rig.ID, Normalized: norm}}, nil
}

// Source says which tree a tape was made from.
type Source struct{ Revision string }

var sha = regexp.MustCompile(`^[0-9a-f]{40}$`)

// SourceFromEnv reads the revision the caller names in TOGO_TAPE_REVISION
// (git rev-parse HEAD) and checks it against the tree at dir: it must be
// the checked-out commit, and the tree must be clean unless
// TOGO_TAPE_ALLOW_DIRTY=1.
func SourceFromEnv(dir string) (Source, error) {
	rev := os.Getenv("TOGO_TAPE_REVISION")
	if !sha.MatchString(rev) {
		return Source{}, errors.New("set TOGO_TAPE_REVISION to the full commit (git rev-parse HEAD): a tape without its revision is not written")
	}
	head, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return Source{}, err
	}
	if head != rev {
		return Source{}, fmt.Errorf("TOGO_TAPE_REVISION is %s but the tree is at %s", rev, head)
	}
	if os.Getenv("TOGO_TAPE_ALLOW_DIRTY") != "1" {
		out, err := git(dir, "status", "--porcelain")
		if err != nil {
			return Source{}, err
		}
		if out != "" {
			return Source{}, errors.New("the tree has uncommitted changes: commit them, or set TOGO_TAPE_ALLOW_DIRTY=1 for a tape that is not evidence")
		}
	}
	return Source{Revision: rev}, nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Write puts the tape and its manifest in dir as base.jsonl and base.json.
func (t *Tape) Write(dir, base string, src Source) error {
	if !sha.MatchString(src.Revision) {
		return errors.New("tape: no helper revision")
	}
	m := t.Manifest
	m.HelperRevision = src.Revision
	if m.Normalized == nil {
		m.Normalized = []string{}
	}
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, base+".jsonl"), t.Bytes(), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, base+".json"), append(manifest, '\n'), 0o644)
}
