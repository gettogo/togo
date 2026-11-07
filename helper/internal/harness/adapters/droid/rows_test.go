package droid

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Resume verifies against the session id the mapper keeps; compress asks
// once, in Droid's own dialog.
func TestResumeVerifyAndOneCompressConfirm(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if v := a.Actions["resume"].Verify; v == nil || v.Select != "$state.harness_session" {
		t.Errorf("verify %+v", v)
	}
	if c := a.Actions["compact"].Confirm; c != "" {
		t.Errorf("compact asks twice: %q", c)
	}
	if _, ok := a.Screens["compact"]; ok || a.Status == nil || a.Status.Actions[0] != "compact" {
		t.Error("compact is not inline")
	}
}
