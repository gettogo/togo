package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// ConflictError is a git write refused because the repository is not in the
// state the client saw or cannot be written now. Reason is one of the
// wire.Conflict* git reasons. Nothing in the repository was changed, except
// for wire.ConflictPartial, where Changed lists the paths that did change,
// and wire.ConflictRaced, where Hash is the commit that was made.
type ConflictError struct {
	Reason  string
	Changed []string
	Hash    string
}

func (e *ConflictError) Error() string { return "git write refused: " + e.Reason }

func conflict(reason string) error { return &ConflictError{Reason: reason} }

// CommitResult is the git.commit result: the new HEAD.
type CommitResult struct {
	Hash  string `json:"hash"`
	Short string `json:"short"`
}

// writeArgs come before every subcommand that changes the repository, on
// top of safeArgs. Signing, editors, background maintenance and comment
// characters a repository sets are all overridden.
var writeArgs = []string{
	"-c", "commit.gpgSign=false",
	"-c", "tag.gpgSign=false",
	"-c", "core.editor=true",
	"-c", "filter.lfs.required=false",
	"-c", "gc.auto=0",
	"-c", "maintenance.auto=false",
	"-c", "core.commentChar=#",
	"-c", "user.useConfigOnly=true",
}

type writeOp int

const (
	opStage writeOp = iota
	opUnstage
	opDiscard
)

// acts reports whether op has something to do for e.
func (op writeOp) acts(e entry) bool {
	switch op {
	case opStage:
		return e.kind == '?' || e.kind == 'u' || e.Worktree != "."
	case opUnstage:
		return e.kind != '?' && e.Index != "."
	default:
		return true
	}
}

// repo is a repository checked and ready for one write.
type repo struct {
	root string
	// head is the commit HEAD points at, "" on an unborn branch. Set by prepare.
	head string
}

// gitWrite runs a write subcommand with paths, if any, on stdin as a NUL
// separated pathspec list, so no path is ever read as an option or glob.
func (g *Git) gitWrite(ctx context.Context, r repo, stdin []byte, args ...string) ([]byte, error) {
	argv := append(append([]string{}, writeArgs...), args...)
	out, _, err := g.run(ctx, r.root, stdin, listLimit, argv...)
	if stderrMentions(err, "index.lock") {
		return nil, conflict(wire.ConflictLocked)
	}
	return out, err
}

func nulJoin(paths []string) []byte {
	var b bytes.Buffer
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte(0)
	}
	return b.Bytes()
}

var fromStdin = []string{"--pathspec-from-file=-", "--pathspec-file-nul", "--"}

// open finds the repository at dir and refuses the write when another git
// process holds the index lock.
func (g *Git) open(ctx context.Context, dir string) (repo, error) {
	root, err := g.Toplevel(ctx, dir)
	if err != nil {
		return repo{}, err
	}
	if root == "" {
		return repo{}, conflict(wire.ConflictStale)
	}
	out, _, err := g.Run(ctx, root, 4096, "rev-parse", "--git-path", "index.lock")
	if err != nil {
		return repo{}, err
	}
	lock := strings.TrimRight(string(out), "\n")
	if !filepath.IsAbs(lock) {
		lock = filepath.Join(root, lock)
	}
	if _, err := os.Lstat(lock); err == nil {
		return repo{}, conflict(wire.ConflictLocked)
	}
	return repo{root: root}, nil
}

// checkInside refuses a path whose parent folders are not real folders
// inside root (a symlink there could lead out of the repository) or that
// lies in another repository (a submodule or a nested clone, recognised by
// its .git entry), and a path that names a folder.
func checkInside(root, p string) error {
	parts := strings.Split(p, "/")
	cur := root
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %q cannot be read", fsx.ErrInvalidPath, p)
		}
		last := i == len(parts)-1
		switch {
		case last && info.IsDir():
			return &session.InvalidParamsError{Msg: fmt.Sprintf("%q is a folder; name each file", p)}
		case !last && !info.IsDir():
			return fmt.Errorf("%w: %q is not inside the repository", fsx.ErrInvalidPath, p)
		case !last:
			if _, err := os.Lstat(filepath.Join(cur, ".git")); err == nil {
				return fmt.Errorf("%w: %q is inside another repository", fsx.ErrInvalidPath, p)
			}
		}
	}
	return nil
}

// prepare checks every path against a fresh status and the letters the
// client saw, then the filter attributes, reads HEAD, and last compares the
// fingerprints the client saw. It returns the status entry of each path.
func (g *Git) prepare(ctx context.Context, dir string, op writeOp, paths []string, seen map[string]wire.GitSeen) (repo, []entry, error) {
	r, err := g.open(ctx, dir)
	if err != nil {
		return r, nil, err
	}
	for _, p := range paths {
		if err := checkInside(r.root, p); err != nil {
			return r, nil, err
		}
		// Discard deletes work, so it acts only on files the client saw.
		if _, ok := seen[p]; op == opDiscard && !ok {
			return r, nil, &session.InvalidParamsError{Msg: "seen must list every path"}
		}
	}
	var hdr Status
	all, err := g.scan(ctx, r.root, &hdr)
	if err != nil {
		return r, nil, err
	}
	byPath := map[string]entry{}
	for _, e := range all {
		if _, dup := byPath[e.Path]; !dup {
			byPath[e.Path] = e
		}
	}
	var targets []entry
	var withSeen []string
	for _, p := range paths {
		e, ok := byPath[p]
		if !ok || !op.acts(e) {
			return r, nil, conflict(wire.ConflictStale)
		}
		if s, ok := seen[p]; ok {
			if s.Status != e.letters() {
				return r, nil, conflict(wire.ConflictStale)
			}
			withSeen = append(withSeen, p)
		}
		targets = append(targets, e)
	}
	checked := paths
	for _, e := range targets {
		if e.kind == 'u' && op != opStage {
			return r, nil, conflict(wire.ConflictUnmerged)
		}
		orig := renamedFrom(e)
		if orig == "" || op == opStage {
			continue
		}
		if op == opDiscard {
			if err := checkOrigPath(r.root, e, orig, seen); err != nil {
				return r, nil, err
			}
			if _, listed := byPath[orig]; listed {
				return r, nil, conflict(wire.ConflictStale)
			}
		}
		checked = append(append([]string{}, checked...), orig)
	}
	if err := g.checkFilters(ctx, r, checked); err != nil {
		return r, nil, err
	}
	if r.head, err = g.headHash(ctx, r.root); err != nil {
		return r, nil, err
	}
	// Fingerprints go last, so the content they vouch for is as close to
	// the write as the checks allow.
	listed := make([]string, len(all))
	for i, e := range all {
		listed[i] = e.Path
	}
	want := map[string]bool{}
	for _, p := range withSeen {
		want[p] = true
	}
	fps := g.fingerprints(ctx, r.root, listed, want)
	for _, p := range withSeen {
		if seen[p].Fingerprint != fps[p] {
			return r, nil, conflict(wire.ConflictStale)
		}
	}
	return r, targets, nil
}

// checkFilters refuses paths with any filter attribute: writing them would
// need a program the repository names (Git LFS, for one).
func (g *Git) checkFilters(ctx context.Context, r repo, paths []string) error {
	out, _, err := g.run(ctx, r.root, nulJoin(paths), listLimit, "check-attr", "-z", "--stdin", "filter")
	if err != nil {
		return err
	}
	f := strings.Split(string(out), "\x00")
	if len(f) < 3*len(paths) {
		return fmt.Errorf("git check-attr: short output")
	}
	for i := 0; i+2 < len(f); i += 3 {
		if f[i+2] != "unspecified" {
			return conflict(wire.ConflictFiltered)
		}
	}
	return nil
}

// headHash returns the commit HEAD points at, or "" on an unborn branch.
func (g *Git) headHash(ctx context.Context, root string) (string, error) {
	out, _, err := g.Run(ctx, root, 1024, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	var ee *ExitError
	if errors.As(err, &ee) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Stage records the working-tree state of each path in the index, deletions
// included, and returns the status after it.
func (g *Git) Stage(ctx context.Context, dir string, paths []string, seen map[string]wire.GitSeen) (Status, error) {
	r, _, err := g.prepare(ctx, dir, opStage, paths, seen)
	if err != nil {
		return Status{}, err
	}
	args := append([]string{"add", "-A"}, fromStdin...)
	if _, err := g.gitWrite(ctx, r, nulJoin(paths), args...); err != nil {
		return Status{}, err
	}
	return g.Status(ctx, dir)
}

// Unstage puts each path's index entry back to HEAD, or drops it on a
// branch with no commit yet. A staged rename gets its old path back in the
// index as well. The working tree is untouched.
func (g *Git) Unstage(ctx context.Context, dir string, paths []string, seen map[string]wire.GitSeen) (Status, error) {
	r, targets, err := g.prepare(ctx, dir, opUnstage, paths, seen)
	if err != nil {
		return Status{}, err
	}
	args := append([]string{"restore", "--staged"}, fromStdin...)
	if r.head == "" {
		args = append([]string{"rm", "--cached", "-f", "-q"}, fromStdin...)
	}
	// A staged rename is unstaged whole: its old path goes back in the index too.
	all := paths
	for _, e := range targets {
		if orig := renamedFrom(e); orig != "" {
			all = append(append([]string{}, all...), orig)
		}
	}
	if _, err := g.gitWrite(ctx, r, nulJoin(all), args...); err != nil {
		return Status{}, err
	}
	return g.Status(ctx, dir)
}

// Discard drops every change to each path. On a branch with a commit, one
// `restore --source=HEAD --staged --worktree` handles every path git knows:
// a path in HEAD gets its index entry and file back, a path only in the
// index leaves it and its file is deleted. Before the first commit those
// paths leave the index with `rm --cached` and their files are deleted.
// A staged rename is undone: its old path is restored from HEAD in the same
// restore, so the new file goes and the old one comes back. That needs the
// old path free on disk and seen by the client (checkOrigPath).
// Untracked files are deleted last, through os.Root. Nothing else is
// touched: no other file, no folder and no ignored file.
//
// The single index write takes the lock before changing anything, so a
// held lock is refused with nothing changed. A failure after it is
// reported as wire.ConflictPartial with the paths that did change.
func (g *Git) Discard(ctx context.Context, dir string, paths []string, seen map[string]wire.GitSeen) (Status, error) {
	r, targets, err := g.prepare(ctx, dir, opDiscard, paths, seen)
	if err != nil {
		return Status{}, err
	}
	head := r.head
	var indexed, remove, restored []string
	for _, e := range targets {
		if e.kind == '?' {
			remove = append(remove, e.Path)
		} else {
			indexed = append(indexed, e.Path)
			if orig := renamedFrom(e); orig != "" && head != "" {
				indexed = append(indexed, orig)
				restored = append(restored, orig)
			}
		}
	}
	if len(indexed) > 0 {
		args := append([]string{"restore", "--source=HEAD", "--staged", "--worktree", "--no-recurse-submodules"}, fromStdin...)
		if head == "" {
			args = append([]string{"rm", "--cached", "-f", "-q"}, fromStdin...)
			remove = append(remove, indexed...)
		}
		if _, err := g.gitWrite(ctx, r, nulJoin(indexed), args...); err != nil {
			var ce *ConflictError
			if errors.As(err, &ce) {
				return Status{}, err // locked: refused before any change
			}
			return Status{}, g.partial(ctx, r, targets, restored, err)
		}
	}
	if err := removeFiles(r.root, remove); err != nil {
		return Status{}, g.partial(ctx, r, targets, restored, err)
	}
	return g.Status(ctx, dir)
}

// checkOrigPath makes sure restoring the old path of rename e can only bring
// back a file the rename removed. A rename that only changes letter case is
// refused: on a case-insensitive disk both names are one file. The old path
// must be what the client saw paired with e, and nothing may be on disk
// there, under any letter case: not a new file, an ignored file, a link or
// a folder.
func checkOrigPath(root string, e entry, orig string, seen map[string]wire.GitSeen) error {
	if strings.EqualFold(orig, e.Path) {
		return conflict(wire.ConflictCaseRename)
	}
	if s, ok := seen[e.Path]; ok {
		o, ok := seen[orig]
		if !ok || o.Status != s.Status || o.Fingerprint != "" {
			return conflict(wire.ConflictStale)
		}
	}
	if err := checkInside(root, orig); err != nil {
		return err
	}
	rt, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer rt.Close()
	if _, err := rt.Lstat(orig); !errors.Is(err, fs.ErrNotExist) {
		return conflict(wire.ConflictStale)
	}
	return nil
}

// renamedFrom returns the old path of a rename staged in the index, or "".
// A copy leaves its source in place, so it has nothing to bring back.
func renamedFrom(e entry) string {
	if e.kind != '2' || e.Index != "R" || e.OrigPath == nil {
		return ""
	}
	return *e.OrigPath
}

// partial reports a write that failed after it may have changed some of
// targets: the paths whose status letters now differ, or that left the
// status, are listed. When nothing changed the original error stands.
func (g *Git) partial(ctx context.Context, r repo, targets []entry, restored []string, cause error) error {
	var hdr Status
	all, err := g.scan(ctx, r.root, &hdr)
	if err != nil {
		return cause
	}
	now := map[string]string{}
	for _, e := range all {
		if _, dup := now[e.Path]; !dup {
			now[e.Path] = e.letters()
		}
	}
	var changed []string
	for _, e := range targets {
		if l, ok := now[e.Path]; !ok || l != e.letters() {
			changed = append(changed, e.Path)
		}
	}
	// A rename's old path was not listed before the write; listed now, it changed.
	for _, p := range restored {
		if _, ok := now[p]; ok {
			changed = append(changed, p)
		}
	}
	if len(changed) == 0 {
		return cause
	}
	return &ConflictError{Reason: wire.ConflictPartial, Changed: changed}
}

// removeFiles deletes each file through an os.Root, which never follows a
// link out of root. A symlink is removed itself, never its target.
func removeFiles(root string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	rt, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer rt.Close()
	for _, p := range paths {
		info, err := rt.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return &session.InvalidParamsError{Msg: fmt.Sprintf("%q is a folder; name each file", p)}
		}
		if err := rt.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Commit commits the index with message, or replaces HEAD with amend.
// head, when set, is the commit the client saw as HEAD; a different HEAD is
// stale. Hooks and signing never run.
func (g *Git) Commit(ctx context.Context, dir, message string, amend bool, head string) (CommitResult, error) {
	var res CommitResult
	r, err := g.open(ctx, dir)
	if err != nil {
		return res, err
	}
	cur, err := g.headHash(ctx, r.root)
	if err != nil {
		return res, err
	}
	switch {
	case amend && cur == "":
		return res, conflict(wire.ConflictUnbornAmend)
	case head != "" && head != cur:
		return res, conflict(wire.ConflictStale)
	}
	if !amend {
		_, _, err := g.Run(ctx, r.root, 1024,
			"diff", "--cached", "--quiet", "--no-ext-diff", "--no-textconv", ignoreSubmodules, "--")
		var ee *ExitError
		if err == nil {
			return res, conflict(wire.ConflictNothingStaged)
		}
		if !errors.As(err, &ee) || ee.Code != 1 {
			return res, err
		}
	}
	var oldParents []string
	if amend {
		if oldParents, err = g.parents(ctx, r.root, cur); err != nil {
			return res, err
		}
	}
	args := []string{"commit", "-q", "--no-verify", "--no-gpg-sign", "--cleanup=strip", "-F", "-"}
	if amend {
		args = append(args, "--amend", "--no-post-rewrite")
	}
	_, err = g.gitWrite(ctx, r, []byte(message), args...)
	switch {
	case stderrMentions(err, "user.email", "user.name", "Please tell me who you are", "empty ident"):
		return res, conflict(wire.ConflictIdentity)
	case stderrMentions(err, "unmerged files"):
		return res, conflict(wire.ConflictUnmerged)
	case stderrMentions(err, "empty commit message"):
		return res, &session.InvalidParamsError{Msg: "the message is empty once comment lines are removed"}
	case err != nil:
		return res, err
	}
	if res.Hash, err = g.headHash(ctx, r.root); err != nil {
		return res, err
	}
	out, _, err := g.Run(ctx, r.root, 1024, "rev-parse", "--short", res.Hash)
	if err != nil {
		return res, err
	}
	res.Short = strings.TrimSpace(string(out))
	// git commit moves HEAD with a compare-and-swap against the HEAD it
	// read, but another process can move HEAD between the check above and
	// that read. The new commit must sit where the user expected: on the
	// checked HEAD, or for an amend, on the replaced commit's parents.
	got, err := g.parents(ctx, r.root, res.Hash)
	if err != nil {
		return res, err
	}
	want := oldParents
	if !amend && cur != "" {
		want = []string{cur}
		got = got[:min(len(got), 1)]
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		return res, &ConflictError{Reason: wire.ConflictRaced, Hash: res.Hash}
	}
	return res, nil
}

// parents returns the parent hashes of commit hash, first parent first.
func (g *Git) parents(ctx context.Context, root, hash string) ([]string, error) {
	out, _, err := g.Run(ctx, root, 4096, "rev-list", "--parents", "-n", "1", "--end-of-options", hash)
	if err != nil {
		return nil, err
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return nil, fmt.Errorf("git rev-list: no commit")
	}
	return f[1:], nil
}
