package gitx

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gettogo/togo/helper/internal/fsx"
)

// Commit is one git.log entry.
type Commit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

// Log returns up to limit commits reachable from ref, newest first. An empty
// ref means HEAD, which in a repository without commits yields no commits.
func (g *Git) Log(ctx context.Context, dir, ref string, limit int) ([]Commit, error) {
	commits := []Commit{}
	if ref != "" {
		if err := CheckRef(ref); err != nil {
			return commits, err
		}
	}
	root, err := g.Toplevel(ctx, dir)
	if err != nil || root == "" {
		return commits, err
	}
	target := "HEAD"
	if ref != "" {
		target = ref
	}
	hash, err := g.ResolveCommit(ctx, dir, target)
	if err != nil {
		if ref == "" && errors.Is(err, fsx.ErrInvalidPath) {
			return commits, nil
		}
		return commits, err
	}
	out, _, err := g.Run(ctx, dir, listLimit, "log", "--no-color", "--no-show-signature", "-z", "-n", strconv.Itoa(limit),
		"--format=%H%x00%h%x00%an%x00%aI%x00%s", hash, "--")
	if err != nil {
		return commits, err
	}
	f := bytes.Split(out, []byte{0})
	for i := 0; i+5 <= len(f); i += 5 {
		commits = append(commits, Commit{
			Hash: string(f[i]), Short: string(f[i+1]), Author: string(f[i+2]),
			Date: utcSeconds(string(f[i+3])), Subject: string(f[i+4]),
		})
	}
	return commits, nil
}

func utcSeconds(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
