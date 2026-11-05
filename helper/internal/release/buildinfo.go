package release

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// BuildInfoName is the build record's file name.
const BuildInfoName = "BUILDINFO.json"

// BuildInfo records what a release was built from. Binaries maps each target
// ("os/arch") to the SHA-256 of its binary before any signing.
type BuildInfo struct {
	Version         string
	Commit          string
	GoVersion       string
	Dirty           bool // built from a tree with uncommitted changes
	SourceDateEpoch int64
	Binaries        map[string]string
}

// WriteBuildInfo writes BUILDINFO.json in dir with sorted keys and a trailing
// newline.
func WriteBuildInfo(dir string, b BuildInfo) (string, error) {
	bins := b.Binaries
	if bins == nil {
		bins = map[string]string{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Maps marshal with sorted keys.
	err := enc.Encode(map[string]any{
		"version":         b.Version,
		"commit":          b.Commit,
		"dirty":           b.Dirty,
		"goVersion":       b.GoVersion,
		"sourceDateEpoch": b.SourceDateEpoch,
		"binaries":        bins,
	})
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, BuildInfoName)
	return path, os.WriteFile(path, buf.Bytes(), 0o644)
}
