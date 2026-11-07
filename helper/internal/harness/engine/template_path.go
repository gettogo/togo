package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Filters that build the per-folder names harnesses give their session
// folders: Pi's "--Users-me-proj--" ({{.cwd | replace "/" "-"}}) and Kimi's
// "wd_<name>_<hash>" ({{slice (sha256 .cwd) 0 12}}).

// replace replaces every old with new in s; the subject comes last so the
// filter reads well in a pipeline.
func replace(old, new string, s any) string {
	return strings.ReplaceAll(filterText(s), old, new)
}

// sha256Hex is the hex SHA-256 of a value's text.
func sha256Hex(s any) string {
	sum := sha256.Sum256([]byte(filterText(s)))
	return hex.EncodeToString(sum[:])
}

// filterText is a value's text; a missing value is empty.
func filterText(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
