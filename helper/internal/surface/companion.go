package surface

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/state"
)

// Companion lease renewals must arrive before the independent in-agent expiry.
const (
	RendererLease     = 6 * time.Second
	RendererRenew     = 2 * time.Second
	CompanionWait     = 2 * time.Second
	MaxCompanionBytes = 32 << 10
)

//go:embed omp.mjs
var companionFiles embed.FS

var generationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Companions materializes optional, per-run harness companions under TOGO_HOME.
type Companions struct{ Home string }

// CompanionConfig is private launch configuration; Token never goes over RPC.
type CompanionConfig struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	Generation string `json:"generation"`
	Token      string `json:"token"`
	Path       string `json:"-"`
}

// CompanionState reports the actual original runtime, not a discovery child.
type CompanionState struct {
	Version      int    `json:"version"`
	Generation   string `json:"generation"`
	PID          int    `json:"pid"`
	Pane         string `json:"pane"`
	Conversation string `json:"conversation"`
	SessionFile  string `json:"sessionFile"`
	Cwd          string `json:"cwd"`
	Epoch        uint64 `json:"epoch"`
	Available    bool   `json:"available"`
	Reason       string `json:"reason,omitempty"`
	Native       bool   `json:"native"`
	Owner        string `json:"owner"`
	Seq          uint64 `json:"seq"`
}

// PrepareLaunch preserves the user's argv and profile. Only supported
// interactive OMP launches receive the explicit companion extension.
func (c *Companions) PrepareLaunch(_ context.Context, id, agent string, launch session.Launch) (session.Launch, error) {
	if agent != "omp" || !interactiveOMP(launch.Args) {
		return launch, nil
	}
	root, err := c.sessionDir(id)
	if err != nil {
		return launch, err
	}
	config := CompanionConfig{Version: 1, ID: id, Generation: randomHex(16), Token: randomHex(32)}
	dir := filepath.Join(root, config.Generation)
	config.Path = filepath.Join(dir, "config.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return launch, err
	}
	module := filepath.Join(c.Home, "integrations", "omp-surface-v1.mjs")
	if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
		return launch, err
	}
	code, _ := companionFiles.ReadFile("omp.mjs")
	if err := state.WriteFile(module, code); err != nil {
		return launch, err
	}
	if err := writeJSON(config.Path, config); err != nil {
		return launch, err
	}
	if err := writeJSON(filepath.Join(root, "current.json"), map[string]string{"generation": config.Generation}); err != nil {
		return launch, err
	}
	args := slices.Clone(launch.Args)
	at := slices.Index(args, "--")
	if at < 0 {
		at = len(args)
	}
	launch.Args = slices.Insert(args, at, "-e", module)
	launch.Env = maps.Clone(launch.Env)
	if launch.Env == nil {
		launch.Env = map[string]string{}
	}
	launch.Env["TOGO_SURFACE_CONFIG"] = config.Path
	return launch, nil
}

func interactiveOMP(args []string) bool {
	if len(args) > 0 && slices.Contains([]string{"config", "models", "ps", "stats", "update", "doctor", "auth", "setup"}, args[0]) {
		return false
	}
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if slices.Contains([]string{"--no-extensions", "--mode", "--print", "-p", "--help", "-h", "--version", "--list-models"}, arg) || strings.HasPrefix(arg, "--mode=") {
			return false
		}
	}
	return true
}

// Current reloads the private generation after a helper restart.
func (c *Companions) Current(id string) (CompanionConfig, error) {
	root, err := c.sessionDir(id)
	if err != nil {
		return CompanionConfig{}, err
	}
	var current struct {
		Generation string `json:"generation"`
	}
	if err := readJSON(filepath.Join(root, "current.json"), &current); err != nil || !generationPattern.MatchString(current.Generation) {
		return CompanionConfig{}, errors.New("surface: no current companion")
	}
	path := filepath.Join(root, current.Generation, "config.json")
	var config CompanionConfig
	if readJSON(path, &config) != nil || config.Version != 1 || config.ID != id || config.Generation != current.Generation || len(config.Token) != 64 {
		return CompanionConfig{}, errors.New("surface: invalid companion configuration")
	}
	config.Path = path
	return config, nil
}

// State reads bounded metadata and rejects another process generation's file.
func (c *Companions) State(config CompanionConfig) (CompanionState, error) {
	var result CompanionState
	if err := readJSON(filepath.Join(filepath.Dir(config.Path), "state.json"), &result); err != nil {
		return result, err
	}
	if result.Generation != config.Generation || result.PID <= 1 || !strings.HasPrefix(result.Pane, "%") {
		return CompanionState{}, errors.New("surface: companion identity changed")
	}
	return result, nil
}

// Command changes or renews a renderer lease. Callers serialize commands for
// this run, and validate process/pane ownership before issuing one.
func (c *Companions) Command(ctx context.Context, config CompanionConfig, previous CompanionState, mode, owner string) (CompanionState, error) {
	request := struct {
		CompanionConfig
		Seq          uint64 `json:"seq"`
		Mode         string `json:"mode"`
		Owner        string `json:"owner"`
		Conversation string `json:"conversation"`
		Epoch        uint64 `json:"epoch"`
		LeaseMS      int64  `json:"leaseMs"`
	}{config, previous.Seq + 1, mode, owner, previous.Conversation, previous.Epoch, RendererLease.Milliseconds()}
	if err := writeJSON(filepath.Join(filepath.Dir(config.Path), "command.json"), request); err != nil {
		return CompanionState{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, CompanionWait)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		current, err := c.State(config)
		if err == nil && current.Seq == request.Seq && current.Conversation == previous.Conversation && current.Epoch == previous.Epoch {
			if current.Reason != "" || !current.Available {
				return current, fmt.Errorf("surface: companion could not change renderer (%s)", current.Reason)
			}
			if current.Native == (mode != "ansi") {
				return current, nil
			}
		}
		select {
		case <-ctx.Done():
			return CompanionState{}, fmt.Errorf("surface: companion did not confirm: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

func (c *Companions) sessionDir(id string) (string, error) {
	managed, err := session.ValidateID(id)
	if err != nil || !managed || !filepath.IsAbs(c.Home) {
		return "", errors.New("surface: invalid companion session")
	}
	return filepath.Join(c.Home, "run", "surfaces", id), nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(path string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return state.WriteFile(path, b)
}

func readJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > MaxCompanionBytes {
		return errors.New("surface: invalid companion file")
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxCompanionBytes+1))
	if err != nil || len(b) > MaxCompanionBytes {
		return errors.New("surface: companion file too large")
	}
	return json.Unmarshal(b, value)
}
