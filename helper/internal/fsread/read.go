// Package fsread serves fs.read and the ref and root forms of fs.listDir:
// bounded reads of files and directory trees inside a session's working
// directory, or with root inside the repository holding it, either from the
// working tree or from a commit.
package fsread

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/fsx"
	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

// binarySniff is how much of a file is checked for NUL bytes.
const binarySniff = 8 << 10

// Service reads files for sessions.
type Service struct {
	Sessions gitx.Resolver
	Git      gitx.Git
}

// Result is the fs.read result.
type Result struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Encoding  string `json:"encoding"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

// Methods returns fs.read and the fs.listDir override for API.Ext.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{
		wire.MethodFsRead: s.read,
		"fs.listDir":      s.listDir,
	}
}

func (s *Service) read(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		ID   string `json:"id"`
		Path string `json:"path"`
		Ref  string `json:"ref"`
		Root bool   `json:"root"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	dir, err := s.baseDir(ctx, p.ID, p.Root)
	if err != nil {
		return nil, err
	}
	rel, err := gitx.CleanRel(p.Path)
	if err != nil {
		return nil, err
	}
	if p.Ref != "" {
		return s.readAtRef(ctx, dir, rel, p.Ref)
	}
	return readWorkingTree(dir, rel)
}

// baseDir returns the folder paths are relative to: the session cwd, or with
// root the top of the repository holding it, where git.status paths start.
func (s *Service) baseDir(ctx context.Context, id string, root bool) (string, error) {
	dir, err := gitx.SessionDir(ctx, s.Sessions, id)
	if err != nil || !root {
		return dir, err
	}
	top, err := s.Git.Toplevel(ctx, dir)
	if err != nil {
		return "", err
	}
	if top == "" {
		return "", fmt.Errorf("%w: the session folder is not in a repository", fsx.ErrInvalidPath)
	}
	return top, nil
}

func readWorkingTree(dir, rel string) (Result, error) {
	full, err := resolveInside(dir, rel)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return Result{}, fmt.Errorf("%w: %s is not a readable file", fsx.ErrInvalidPath, rel)
	}
	f, err := os.Open(full)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", fsx.ErrInvalidPath, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, gitx.MaxOutput))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", fsx.ErrInvalidPath, err)
	}
	return encode(rel, info.Size(), data), nil
}

// resolveInside joins rel onto dir and follows symlinks, refusing a result
// that is not under the resolved dir. The caller opens the returned path.
func resolveInside(dir, rel string) (string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("%w: %w", fsx.ErrInvalidPath, err)
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, rel))
	if err != nil {
		return "", fmt.Errorf("%w: %w", fsx.ErrInvalidPath, err)
	}
	if !filepath.IsLocal(mustRel(root, full)) {
		return "", fmt.Errorf("%w: %s resolves outside the session directory", fsx.ErrInvalidPath, rel)
	}
	return full, nil
}

func mustRel(root, full string) string {
	r, err := filepath.Rel(root, full)
	if err != nil {
		return ".."
	}
	return r
}

func (s *Service) readAtRef(ctx context.Context, dir, rel, ref string) (Result, error) {
	hash, err := s.Git.ResolveCommit(ctx, dir, ref)
	if err != nil {
		return Result{}, err
	}
	obj := hash + ":./" + filepath.ToSlash(rel)
	typ, _, err := s.Git.Run(ctx, dir, 64, "cat-file", "-t", obj)
	if err != nil || strings.TrimSpace(string(typ)) != "blob" {
		return Result{}, fmt.Errorf("%w: %s is not a file at that ref", fsx.ErrInvalidPath, rel)
	}
	size, _, err := s.Git.Run(ctx, dir, 32, "cat-file", "-s", obj)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s", fsx.ErrInvalidPath, rel)
	}
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(size)), "%d", &n); err != nil {
		return Result{}, fmt.Errorf("cat-file size: %w", err)
	}
	data, _, err := s.Git.Run(ctx, dir, gitx.MaxOutput, "cat-file", "blob", obj)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s", fsx.ErrInvalidPath, rel)
	}
	return encode(rel, n, data), nil
}

// encode builds the result for data, the first bytes of a file of the given
// size. Text is returned as is; anything else as base64url without padding.
func encode(rel string, size int64, data []byte) Result {
	r := Result{Path: filepath.ToSlash(rel), Size: size, Truncated: size > int64(len(data))}
	if r.Truncated {
		data = trimPartialRune(data)
	}
	head := data[:min(len(data), binarySniff)]
	if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(data) {
		r.Encoding = "base64"
		r.Content = base64.RawURLEncoding.EncodeToString(data)
		return r
	}
	r.Encoding = "utf8"
	r.Content = string(data)
	return r
}

// trimPartialRune drops a multi-byte character cut by the size cap so a
// truncated text file is still valid UTF-8.
func trimPartialRune(data []byte) []byte {
	for i := 1; i <= utf8.UTFMax-1 && i <= len(data); i++ {
		c := data[len(data)-i]
		if utf8.RuneStart(c) {
			if !utf8.FullRune(data[len(data)-i:]) {
				return data[:len(data)-i]
			}
			break
		}
	}
	return data
}

func (s *Service) listDir(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		Path         *string `json:"path"`
		ShowHidden   bool    `json:"showHidden"`
		IncludeFiles bool    `json:"includeFiles"`
		ID           string  `json:"id"`
		Ref          string  `json:"ref"`
		Root         bool    `json:"root"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	switch {
	case p.Path == nil:
		return nil, &session.InvalidParamsError{Msg: "path is required"}
	case p.Root && p.ID == "":
		return nil, &session.InvalidParamsError{Msg: "root needs id"}
	}
	if p.Ref == "" && !p.Root {
		list := fsx.ListDir
		if p.IncludeFiles {
			list = fsx.ListFiles
		}
		dir, entries, err := list(*p.Path, p.ShowHidden)
		if err != nil {
			return nil, err
		}
		if entries == nil {
			entries = []fsx.Entry{}
		}
		return map[string]any{"path": dir, "entries": entries}, nil
	}
	dir, err := s.baseDir(ctx, p.ID, p.Root)
	if err != nil {
		return nil, err
	}
	rel, err := gitx.CleanRel(*p.Path)
	if err != nil {
		return nil, err
	}
	if p.Ref == "" {
		return listWorkingTree(dir, rel, p.ShowHidden, p.IncludeFiles)
	}
	entries, err := s.listAtRef(ctx, dir, rel, p.Ref, p.ShowHidden, p.IncludeFiles)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": filepath.Join(dir, rel), "entries": entries}, nil
}

// listWorkingTree lists the folder rel below dir on disk, refusing one that
// resolves outside dir.
func listWorkingTree(dir, rel string, showHidden, includeFiles bool) (any, error) {
	full, err := resolveInside(dir, rel)
	if err != nil {
		return nil, err
	}
	list := fsx.ListDir
	if includeFiles {
		list = fsx.ListFiles
	}
	path, entries, err := list(full, showHidden)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []fsx.Entry{}
	}
	return map[string]any{"path": path, "entries": entries}, nil
}

func (s *Service) listAtRef(ctx context.Context, dir, rel, ref string, showHidden, includeFiles bool) ([]fsx.Entry, error) {
	hash, err := s.Git.ResolveCommit(ctx, dir, ref)
	if err != nil {
		return nil, err
	}
	args := []string{"ls-tree", "-z", "-d"}
	if includeFiles {
		args = []string{"ls-tree", "-z", "-l"}
	}
	out, _, err := s.Git.Run(ctx, dir, 4<<20, append(args, hash+":./"+filepath.ToSlash(rel))...)
	var ee *gitx.ExitError
	if errors.As(err, &ee) {
		return nil, fmt.Errorf("%w: %s is not a directory at that ref", fsx.ErrInvalidPath, rel)
	}
	if err != nil {
		return nil, err
	}
	entries := []fsx.Entry{}
	for _, rec := range bytes.Split(out, []byte{0}) {
		meta, name, ok := bytes.Cut(rec, []byte{'\t'})
		if !ok || (!showHidden && bytes.HasPrefix(name, []byte("."))) {
			continue
		}
		if !includeFiles {
			entries = append(entries, fsx.Entry{Name: string(name), Dir: true})
			continue
		}
		if e, ok := treeEntry(string(name), meta); ok {
			entries = append(entries, e)
		}
	}
	return fsx.Sorted(entries), nil
}

// treeEntry reads one `git ls-tree -l` record: "<mode> <type> <object> <size>".
// Submodules (mode 160000) are left out.
func treeEntry(name string, meta []byte) (fsx.Entry, bool) {
	fields := strings.Fields(string(meta))
	if len(fields) != 4 {
		return fsx.Entry{}, false
	}
	switch mode := fields[0]; {
	case mode == "040000":
		return fsx.Entry{Name: name, Dir: true, Kind: fsx.KindDir}, true
	case mode == "120000":
		return fsx.Entry{Name: name, Kind: fsx.KindSymlink}, true
	case fields[1] == "blob":
		size, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return fsx.Entry{}, false
		}
		return fsx.Entry{Name: name, Kind: fsx.KindFile, Size: &size}, true
	}
	return fsx.Entry{}, false
}
