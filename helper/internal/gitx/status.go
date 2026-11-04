package gitx

import (
	"bytes"
	"context"
	"strconv"
	"strings"
)

const maxStatusFiles = 5000

// FileStatus is one changed path. Index and Worktree are porcelain v2 status
// letters with "." for unchanged; untracked and ignored files carry "?".
type FileStatus struct {
	Path     string  `json:"path"`
	OrigPath *string `json:"origPath"`
	Index    string  `json:"index"`
	Worktree string  `json:"worktree"`
}

// Status is the git.status result. Paths are relative to the repository root.
type Status struct {
	Repo   bool         `json:"repo"`
	Root   *string      `json:"root"`
	Branch *string      `json:"branch"`
	Ahead  int          `json:"ahead"`
	Behind int          `json:"behind"`
	Files  []FileStatus `json:"files"`
}

// Status reports the repository state at dir.
func (g *Git) Status(ctx context.Context, dir string) (Status, error) {
	st := Status{Files: []FileStatus{}}
	root, err := g.Toplevel(ctx, dir)
	if err != nil || root == "" {
		return st, err
	}
	out, _, err := g.Run(ctx, dir, listLimit, "status", "--porcelain=v2", "-z", "--branch")
	if err != nil {
		return st, err
	}
	st.Repo = true
	st.Root = &root
	parseStatus(out, &st)
	return st, nil
}

func parseStatus(out []byte, st *Status) {
	recs := bytes.Split(out, []byte{0})
	for i := 0; i < len(recs); i++ {
		rec := string(recs[i])
		switch {
		case strings.HasPrefix(rec, "# branch.head "):
			if head := strings.TrimPrefix(rec, "# branch.head "); head != "(detached)" {
				st.Branch = &head
			}
		case strings.HasPrefix(rec, "# branch.ab "):
			f := strings.Fields(rec)
			if len(f) == 4 {
				st.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
				st.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
			}
		case strings.HasPrefix(rec, "1 "):
			st.add(fileFromFields(rec, 9, nil))
		case strings.HasPrefix(rec, "2 "):
			var orig *string
			if i+1 < len(recs) {
				i++
				o := string(recs[i])
				orig = &o
			}
			st.add(fileFromFields(rec, 10, orig))
		case strings.HasPrefix(rec, "u "):
			st.add(fileFromFields(rec, 11, nil))
		case strings.HasPrefix(rec, "? "):
			st.add(FileStatus{Path: rec[2:], Index: "?", Worktree: "?"}, true)
		}
	}
}

func (st *Status) add(f FileStatus, ok bool) {
	if ok && len(st.Files) < maxStatusFiles {
		st.Files = append(st.Files, f)
	}
}

// fileFromFields reads a record of n space-separated fields whose last field
// is the path, which may itself contain spaces.
func fileFromFields(rec string, n int, orig *string) (FileStatus, bool) {
	f := strings.SplitN(rec, " ", n)
	if len(f) != n || len(f[1]) != 2 {
		return FileStatus{}, false
	}
	return FileStatus{Path: f[n-1], OrigPath: orig, Index: f[1][:1], Worktree: f[1][1:]}, true
}
