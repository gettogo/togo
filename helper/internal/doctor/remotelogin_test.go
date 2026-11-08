package doctor

import (
	"context"
	"strings"
	"testing"
)

func TestRemoteLoginOffIsNamed(t *testing.T) {
	e := Env{GOOS: "darwin", SSHListening: func(context.Context) bool { return false }}
	c := checkRemoteLogin(context.Background(), e)
	if c.Status != StatusMissing || !strings.Contains(c.Fix, "Remote Login") {
		t.Fatalf("check %+v", c)
	}
	e.SSHListening = func(context.Context) bool { return true }
	if c := checkRemoteLogin(context.Background(), e); c.Status != StatusOK {
		t.Fatalf("check %+v", c)
	}
}
