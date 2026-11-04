package gitx

import (
	"bytes"
	"context"
	"strings"
)

// Diff is the git.diff result.
type Diff struct {
	Diff      string   `json:"diff"`
	Truncated bool     `json:"truncated"`
	Binary    []string `json:"binary"`
}

// DiffOptions selects what git.diff compares. Path is already validated.
type DiffOptions struct {
	Path   string
	Staged bool
	Ref    string
}

// Diff returns the unified diff at dir. Outside a repository it is empty.
func (g *Git) Diff(ctx context.Context, dir string, o DiffOptions) (Diff, error) {
	res := Diff{Binary: []string{}}
	if o.Ref != "" {
		if err := CheckRef(o.Ref); err != nil {
			return res, err
		}
	}
	root, err := g.Toplevel(ctx, dir)
	if err != nil || root == "" {
		return res, err
	}
	args := []string{"--no-ext-diff", "--no-textconv", "--no-color"}
	if o.Staged {
		args = append(args, "--cached")
	}
	if o.Ref != "" {
		hash, err := g.ResolveCommit(ctx, dir, o.Ref)
		if err != nil {
			return res, err
		}
		args = append(args, hash)
	}
	pathspec := []string{"--"}
	if o.Path != "" {
		pathspec = append(pathspec, o.Path)
	}
	patch := append(append([]string{"diff"}, args...), pathspec...)
	out, truncated, err := g.Run(ctx, dir, MaxOutput, patch...)
	if err != nil {
		return res, err
	}
	res.Diff, res.Truncated = strings.ToValidUTF8(string(out), "�"), truncated
	// numstat repeats the whole diff, so it runs only when binary files can exist.
	if !truncated && !bytes.Contains(out, []byte("Binary files ")) {
		return res, nil
	}
	stat := append(append([]string{"diff", "--numstat", "-z"}, args...), pathspec...)
	out, _, err = g.Run(ctx, dir, listLimit, stat...)
	if err != nil {
		return res, err
	}
	res.Binary = binaryPaths(out)
	return res, nil
}

// binaryPaths reads `diff --numstat -z`, where binary files report "-" for
// both counts. A rename record has an empty path field followed by the old
// and new names.
func binaryPaths(out []byte) []string {
	paths := []string{}
	recs := bytes.Split(out, []byte{0})
	for i := 0; i < len(recs); i++ {
		f := strings.SplitN(string(recs[i]), "\t", 3)
		if len(f) != 3 {
			continue
		}
		path := f[2]
		if path == "" && i+2 < len(recs) {
			path = string(recs[i+2])
			i += 2
		}
		if f[0] == "-" && f[1] == "-" {
			paths = append(paths, path)
		}
	}
	return paths
}
