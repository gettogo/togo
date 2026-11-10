package gitx

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
)

// maxFiles caps a git.files answer; past it the list says it is truncated.
const maxFiles = 50000

// Files is a git.files answer: every file of the repository, relative to its
// root, sorted bytewise.
type Files struct {
	Paths     []string `json:"paths"`
	Truncated bool     `json:"truncated"`
}

type filesParams struct {
	idParams
	Ref string `json:"ref"`
}

func (s *Service) files(ctx context.Context, raw json.RawMessage) (any, error) {
	var p filesParams
	dir, err := s.begin(ctx, raw, &p, func() string { return p.ID })
	if err != nil {
		return nil, err
	}
	return s.Git.Files(ctx, dir, p.Ref)
}

// Files lists the repository holding dir: without a ref, tracked files and
// untracked ones git does not ignore; with one, the files of that commit.
// Outside a repository the list is empty.
func (g *Git) Files(ctx context.Context, dir, ref string) (Files, error) {
	result := Files{Paths: []string{}}
	if ref != "" {
		if err := CheckRef(ref); err != nil {
			return result, err
		}
	}
	root, err := g.Toplevel(ctx, dir)
	if err != nil || root == "" {
		return result, err
	}
	var out []byte
	var cut bool
	if ref == "" {
		out, cut, err = g.Run(ctx, root, listLimit, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--")
	} else {
		hash, rerr := g.ResolveCommit(ctx, root, ref)
		if rerr != nil {
			return result, rerr
		}
		out, cut, err = g.Run(ctx, root, listLimit, "ls-tree", "-r", "-z", "--name-only", "--full-tree", hash)
	}
	if err != nil {
		return result, err
	}
	parts := bytes.Split(out, []byte{0})
	// The last part is empty after a final NUL, or a name cut short by the limit.
	parts = parts[:len(parts)-1]
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		name := string(p)
		// A conflicted file is listed once per stage.
		if name == "" || seen[name] {
			continue
		}
		if len(result.Paths) == maxFiles {
			cut = true
			break
		}
		seen[name] = true
		result.Paths = append(result.Paths, name)
	}
	sort.Strings(result.Paths)
	result.Truncated = cut
	return result, nil
}
