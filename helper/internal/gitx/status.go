package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gettogo/togo/helper/internal/wire"
)

// FileStatus is one changed path. Index and Worktree are porcelain v2 status
// letters with "." for unchanged; untracked files carry "?". Fingerprint
// identifies the working-tree content and is empty when no file is there.
type FileStatus struct {
	Path        string  `json:"path"`
	OrigPath    *string `json:"origPath"`
	Index       string  `json:"index"`
	Worktree    string  `json:"worktree"`
	Fingerprint string  `json:"fingerprint,omitempty"`
}

// Status is the git.status result. Paths are relative to the repository
// root. Truncated is true when files beyond wire.MaxStatusFiles were left out.
type Status struct {
	Repo      bool         `json:"repo"`
	Root      *string      `json:"root"`
	Branch    *string      `json:"branch"`
	Ahead     int          `json:"ahead"`
	Behind    int          `json:"behind"`
	Truncated bool         `json:"truncated"`
	Files     []FileStatus `json:"files"`
}

// entry is a status record with what a write needs beyond the wire shape.
type entry struct {
	FileStatus
	// kind is the porcelain v2 record type: '1' ordinary, '2' renamed or
	// copied, 'u' unmerged, '?' untracked.
	kind byte
}

func (e entry) letters() string { return e.Index + e.Worktree }

// Status reports the repository state at dir.
func (g *Git) Status(ctx context.Context, dir string) (Status, error) {
	st := Status{Files: []FileStatus{}}
	root, err := g.Toplevel(ctx, dir)
	if err != nil || root == "" {
		return st, err
	}
	all, err := g.scan(ctx, root, &st)
	if err != nil {
		return st, err
	}
	if len(all) > wire.MaxStatusFiles {
		all, st.Truncated = all[:wire.MaxStatusFiles], true
	}
	paths := make([]string, len(all))
	for i, e := range all {
		paths[i] = e.Path
	}
	fps := g.fingerprints(ctx, root, paths, nil)
	for _, e := range all {
		f := e.FileStatus
		f.Fingerprint = fps[f.Path]
		st.Files = append(st.Files, f)
	}
	st.Repo = true
	st.Root = &root
	return st, nil
}

// scan runs git status at the repository root and returns every record in
// git's order, filling the branch fields of hdr. Untracked files are listed
// one by one, never as a folder.
func (g *Git) scan(ctx context.Context, root string, hdr *Status) ([]entry, error) {
	out, truncated, err := g.Run(ctx, root, listLimit,
		"status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all", ignoreSubmodules)
	if err != nil {
		return nil, err
	}
	if truncated {
		// The last record may be cut short; drop it rather than act on half a path.
		if i := bytes.LastIndexByte(out, 0); i >= 0 {
			out = out[:i]
		}
		hdr.Truncated = true
	}
	return parseStatus(out, hdr), nil
}

func parseStatus(out []byte, st *Status) []entry {
	var all []entry
	add := func(e entry, ok bool) {
		if ok {
			all = append(all, e)
		}
	}
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
			add(fileFromFields(rec, 9, nil))
		case strings.HasPrefix(rec, "2 "):
			var orig *string
			if i+1 < len(recs) {
				i++
				o := string(recs[i])
				orig = &o
			}
			add(fileFromFields(rec, 10, orig))
		case strings.HasPrefix(rec, "u "):
			add(fileFromFields(rec, 11, nil))
		case strings.HasPrefix(rec, "? "):
			add(entry{FileStatus: FileStatus{Path: rec[2:], Index: "?", Worktree: "?"}, kind: '?'}, true)
		}
	}
	return all
}

// fileFromFields reads a record of n space-separated fields whose last field
// is the path, which may itself contain spaces.
func fileFromFields(rec string, n int, orig *string) (entry, bool) {
	f := strings.SplitN(rec, " ", n)
	if len(f) != n || len(f[1]) != 2 {
		return entry{}, false
	}
	return entry{
		FileStatus: FileStatus{Path: f[n-1], OrigPath: orig, Index: f[1][:1], Worktree: f[1][1:]},
		kind:       rec[0],
	}, true
}

// fingerprintBudget caps the bytes one call hashes for fingerprints.
var fingerprintBudget int64 = 256 << 20

// fingerprints returns the fingerprint of each path in want (every path
// when want is nil) that has a file in the working tree under root.
//
// listed is the whole status list in git's order. Regular files up to
// wire.MaxBlobFingerprintBytes get their blob hash while the running total
// of their sizes stays within fingerprintBudget; from the first file that
// would pass it, every file uses lstat size and mtime. Walking the same
// list decides the form the same way whether status or a write asks, so a
// write compares like with like. Folders and missing paths get none.
func (g *Git) fingerprints(ctx context.Context, root string, listed []string, want map[string]bool) map[string]string {
	fps := map[string]string{}
	var hash []string
	left, spent := fingerprintBudget, false
	for _, p := range listed {
		info, err := os.Lstat(filepath.Join(root, p))
		if err != nil || info.IsDir() {
			continue
		}
		blob := info.Mode().IsRegular() && wire.HashesForFingerprint(info.Size())
		if blob && (spent || info.Size() > left) {
			blob, spent = false, true
		}
		if blob {
			left -= info.Size()
		}
		switch {
		case want != nil && !want[p]:
		case blob:
			hash = append(hash, p)
		default:
			fps[p] = wire.StatFingerprint(info.Size(), info.ModTime().UnixNano())
		}
	}
	g.hashBlobs(ctx, root, hash, fps)
	return fps
}

// hashBlobs adds the blob fingerprint of each path to fps with one
// hash-object process. When a file cannot be read (it vanished or is not
// readable) git stops at it: the hashes before it are kept, it gets no
// fingerprint, which a write reads as stale, and the rest are hashed by a
// new process.
func (g *Git) hashBlobs(ctx context.Context, root string, paths []string, fps map[string]string) {
	for len(paths) > 0 {
		var in bytes.Buffer
		for _, p := range paths {
			in.WriteString(cQuote(p))
			in.WriteByte('\n')
		}
		out, _, err := g.RunInput(ctx, root, in.Bytes(), listLimit, "hash-object", "--no-filters", "--stdin-paths")
		lines := strings.Fields(string(out))
		n := min(len(lines), len(paths))
		for i := range n {
			fps[paths[i]] = wire.BlobFingerprint(lines[i])
		}
		var ee *ExitError
		if err == nil || !errors.As(err, &ee) || n >= len(paths) {
			return
		}
		paths = paths[n+1:]
	}
}

// cQuote writes p as a C-style quoted string, the form git's --stdin-paths
// readers unquote, so names with newlines, quotes or a trailing CR survive.
func cQuote(p string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(p); i++ {
		switch c := p[i]; {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, "\\%03o", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
