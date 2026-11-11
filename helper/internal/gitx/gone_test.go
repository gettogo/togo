package gitx_test

import (
	"errors"
	"os"
	"testing"

	"github.com/gettogo/togo/helper/internal/fsx"
)

// A git call for a session whose folder is gone says so, not the exec
// error git's missing working directory would give.
func TestGitCallOnADeletedSessionFolder(t *testing.T) {
	dir := newRepo(t)
	s := service(dir)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	err := call(t, s, "git.status", `{`+sid+`}`, nil)
	if !errors.Is(err, fsx.ErrFolderGone) {
		t.Fatalf("git.status in a deleted folder: %v, want %v", err, fsx.ErrFolderGone)
	}
}
