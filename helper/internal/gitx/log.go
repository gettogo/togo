package gitx

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/fsx"
)

// Commit is one git.log entry. Parents and Files were added later; clients
// that predate them ignore them.
type Commit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
	// Parents are the full hashes of the commit's parents, first parent first.
	Parents []string `json:"parents"`
	// Files is how many files the commit changes against its first parent,
	// only when asked for a graph. Left out for merges and empty commits.
	Files int `json:"files,omitempty"`
}

// LogOptions shapes a log beyond its ref and length.
type LogOptions struct {
	// Graph orders the log topologically, so every commit comes before its
	// parents, and counts each commit's changed files.
	Graph bool
}

// Log returns up to limit commits reachable from ref, newest first. An empty
// ref means HEAD, which in a repository without commits yields no commits.
func (g *Git) Log(ctx context.Context, dir, ref string, limit int, opts ...LogOptions) ([]Commit, error) {
	var opt LogOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
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
	args := []string{"log", "--no-color", "--no-show-signature", "-z", "-n", strconv.Itoa(limit)}
	if opt.Graph {
		args = append(args, "--topo-order")
	}
	args = append(args, "--format=%H%x00%h%x00%an%x00%aI%x00%P%x00%s", hash, "--")
	out, _, err := g.Run(ctx, dir, listLimit, args...)
	if err != nil {
		return commits, err
	}
	f := bytes.Split(out, []byte{0})
	for i := 0; i+6 <= len(f); i += 6 {
		commits = append(commits, Commit{
			Hash: string(f[i]), Short: string(f[i+1]), Author: string(f[i+2]),
			Date: utcSeconds(string(f[i+3])), Parents: strings.Fields(string(f[i+4])), Subject: string(f[i+5]),
		})
	}
	if opt.Graph && len(commits) > 0 {
		// Counts are a nicety: a log that cannot count still lists its commits.
		if counts, err := g.fileCounts(ctx, dir, hash, limit); err == nil {
			for i := range commits {
				commits[i].Files = counts[commits[i].Hash]
			}
		}
	}
	return commits, nil
}

// fileCounts reads how many files each of the first limit commits from hash
// changes, in the same order as a graph log. Merges and empty commits have no
// entry.
func (g *Git) fileCounts(ctx context.Context, dir, hash string, limit int) (map[string]int, error) {
	out, _, err := g.Run(ctx, dir, listLimit, "log", "--no-color", "--no-show-signature", "--topo-order",
		"-n", strconv.Itoa(limit), "--format=%H", "--shortstat", hash, "--")
	if err != nil {
		return nil, err
	}
	return parseFileCounts(string(out)), nil
}

// parseFileCounts reads `git log --format=%H --shortstat` output: a hash line,
// then, for a commit that changes files, a line such as
// " 2 files changed, 3 insertions(+)". Hash lines never start with a space.
func parseFileCounts(out string) map[string]int {
	counts := map[string]int{}
	current := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
		case !strings.HasPrefix(line, " "):
			current = line
		case current != "":
			fields := strings.Fields(line)
			if len(fields) > 1 && strings.HasPrefix(fields[1], "file") {
				if n, err := strconv.Atoi(fields[0]); err == nil {
					counts[current] = n
				}
			}
		}
	}
	return counts
}

func utcSeconds(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
