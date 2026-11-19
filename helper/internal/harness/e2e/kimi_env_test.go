package e2e

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
)

// status adapts a State to read its status block.
type status struct{ enginetest.State }

func (s status) Get() harness.StatusBlock { return statusOf(s.State) }

// kimiEnv is newEnv with HOME set to the test's home: Kimi's mapper reads
// default_plan_mode from ~/.kimi-code/config.toml of the real home folder.
func kimiEnv(t *testing.T, age time.Duration) env {
	e := newEnv(t, age)
	t.Setenv("HOME", e.home)
	return e
}
