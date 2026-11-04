// Package fsread serves fs.read and the ref form of fs.listDir: bounded reads
// of files and directory trees inside a session's working directory, either
// from the working tree or from a commit.
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
	"slices"
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

// maxListEntries mirrors the fs.listDir cap.
const maxListEntries = 2000

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
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	dir, err := gitx.SessionDir(ctx, s.Sessions, p.ID)
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
		Path       *string `json:"path"`
		ShowHidden bool    `json:"showHidden"`
		ID         string  `json:"id"`
		Ref        string  `json:"ref"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Path == nil {
		return nil, &session.InvalidParamsError{Msg: "path is required"}
	}
	if p.Ref == "" {
		dir, entries, err := fsx.ListDir(*p.Path, p.ShowHidden)
		if err != nil {
			return nil, err
		}
		if entries == nil {
			entries = []fsx.Entry{}
		}
		return map[string]any{"path": dir, "entries": entries}, nil
	}
	dir, err := gitx.SessionDir(ctx, s.Sessions, p.ID)
	if err != nil {
		return nil, err
	}
	rel, err := gitx.CleanRel(*p.Path)
	if err != nil {
		return nil, err
	}
	entries, err := s.listAtRef(ctx, dir, rel, p.Ref, p.ShowHidden)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": filepath.Join(dir, rel), "entries": entries}, nil
}

func (s *Service) listAtRef(ctx context.Context, dir, rel, ref string, showHidden bool) ([]fsx.Entry, error) {
	hash, err := s.Git.ResolveCommit(ctx, dir, ref)
	if err != nil {
		return nil, err
	}
	out, _, err := s.Git.Run(ctx, dir, 4<<20, "ls-tree", "-z", "-d", hash+":./"+filepath.ToSlash(rel))
	var ee *gitx.ExitError
	if errors.As(err, &ee) {
		return nil, fmt.Errorf("%w: %s is not a directory at that ref", fsx.ErrInvalidPath, rel)
	}
	if err != nil {
		return nil, err
	}
	entries := []fsx.Entry{}
	for _, rec := range bytes.Split(out, []byte{0}) {
		_, name, ok := bytes.Cut(rec, []byte{'\t'})
		if !ok || (!showHidden && bytes.HasPrefix(name, []byte("."))) {
			continue
		}
		entries = append(entries, fsx.Entry{Name: string(name), Dir: true})
	}
	slices.SortFunc(entries, func(a, b fsx.Entry) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	if len(entries) > maxListEntries {
		entries = entries[:maxListEntries]
	}
	return entries, nil
}
