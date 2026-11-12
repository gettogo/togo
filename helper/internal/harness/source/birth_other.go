//go:build !darwin

package source

import (
	"os"
	"time"
)

// birthTime: the standard library exposes no creation time here.
func birthTime(os.FileInfo) time.Time { return time.Time{} }
