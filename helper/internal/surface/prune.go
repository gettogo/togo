package surface

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// A preparation normally starts in seconds. Give unpublished configurations
// an hour before reclaiming them; a live process always takes precedence.
const pendingCompanionGrace = time.Hour

// Prune reclaims only private generations whose process is confirmed gone.
// Unknown process state and malformed metadata retain their files. In
// particular, a helper restart never removes an original running harness.
func (c *Companions) Prune(ctx context.Context) error {
	if !filepath.IsAbs(c.Home) {
		return errors.New("surface: invalid cleanup home")
	}
	root := filepath.Join(c.Home, "run", "surfaces")
	sessions, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var firstErr error
	for _, entry := range sessions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() {
			continue
		}
		dir, err := c.sessionDir(entry.Name())
		if err != nil {
			continue
		}
		generations, err := os.ReadDir(dir)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, gen := range generations {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !gen.IsDir() || !generationPattern.MatchString(gen.Name()) {
				continue
			}
			path := filepath.Join(dir, gen.Name())
			var config CompanionConfig
			if readJSON(filepath.Join(path, "config.json"), &config) != nil || config.Version != 1 ||
				config.ID != entry.Name() || config.Generation != gen.Name() || len(config.Token) != 64 {
				continue
			}
			config.Path = filepath.Join(path, "config.json")
			state, err := c.State(config)
			remove := false
			if errors.Is(err, os.ErrNotExist) {
				if info, e := os.Stat(config.Path); e == nil {
					remove = time.Since(info.ModTime()) > pendingCompanionGrace
				}
			} else if err == nil {
				// EPERM and every unknown error preserve the generation. PID
				// reuse can retain stale data, never delete a live process's data.
				remove = errors.Is(syscall.Kill(state.PID, 0), syscall.ESRCH)
			}
			if remove {
				if err := os.RemoveAll(path); err != nil {
					if firstErr == nil {
						firstErr = err
					}
				}
			}
		}
	}
	// Parent/current markers stay: deleting them could race a new publication.
	return firstErr
}

// pruneEpochs runs only after this runtime's identity and pane were validated.
// Older epochs cannot be reopened; current/future epochs remain recoverable.
func (c *Companions) pruneEpochs(ctx context.Context, config CompanionConfig, epoch uint64) error {
	root := filepath.Dir(config.Path)
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() {
			continue
		}
		suffix, ok := strings.CutPrefix(entry.Name(), "cache-")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(suffix, 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != suffix || n >= epoch {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
