package fsx

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
)

// ErrFolderGone is wrapped when a folder a session works in no longer exists.
// Starting a process with a missing working directory fails with an
// ENOENT that Go reports against the program ("fork/exec /x/claude: no such
// file or directory"), so callers check first and say what is missing.
var ErrFolderGone = errors.New("session folder no longer exists")

type folderGone struct{ dir string }

func (e folderGone) Error() string { return fmt.Sprintf("%s: %s", ErrFolderGone, e.dir) }

// Is makes the error match ErrFolderGone and, as before for a folder that
// cannot be used, ErrInvalidPath.
func (e folderGone) Is(target error) bool {
	return target == ErrFolderGone || target == ErrInvalidPath
}

// FolderGoneError names dir in an ErrFolderGone error.
func FolderGoneError(dir string) error { return folderGone{dir} }

// IsDir reports whether path is an existing directory (symlinks followed).
func IsDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

var goneLogged sync.Map

// ExecDir returns the directory to start a neutral process in (a version
// check, a one-off query) for a session whose folder is dir: dir itself when
// it exists, else home, else "" (the helper's own directory). It never picks
// a parent of dir: a parent can be another project, and the user chose dir,
// not its surroundings. Anything that acts on the project itself must not
// use this and should fail with FolderGoneError instead. The first time a
// folder is found missing it is logged. An empty dir stays empty.
func ExecDir(dir, home string) string {
	if dir == "" || IsDir(dir) {
		return dir
	}
	if _, loaded := goneLogged.LoadOrStore(dir, true); !loaded {
		log.Printf("session folder %s no longer exists; its checks run from the home folder", dir)
	}
	if home != "" && IsDir(home) {
		return home
	}
	return ""
}
