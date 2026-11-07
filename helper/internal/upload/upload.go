// Package upload serves fs.upload: files the phone sends for an agent to
// read, such as screenshots, written under $TOGO_HOME/uploads and removed
// after a week. It also serves fs.image, which reads a picture the chat
// shows back to the phone.
package upload

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

const (
	// MaxBytes is the largest decoded upload. Its base64url form plus the
	// request envelope stays under the 1 MiB frame limit.
	MaxBytes = 700_000

	// MaxAge is how long an upload is kept.
	MaxAge = 7 * 24 * time.Hour

	// SweepEvery is the interval between sweeps of old uploads.
	SweepEvery = time.Hour

	maxNameLen = 64
	maxExtLen  = 16
	fallback   = "upload"
)

// Resolver finds the session an upload belongs to.
type Resolver interface {
	Resolve(ctx context.Context, bibOrID string) (session.Session, error)
}

// Service writes uploads under Home.
type Service struct {
	// Home is $TOGO_HOME.
	Home     string
	Sessions Resolver
	// Now is a test seam; nil means time.Now.
	Now func() time.Time
	// Logf receives sweep failures. Nil discards them.
	Logf func(format string, args ...any)
}

// Methods returns fs.upload for API.Ext.
func (s *Service) Methods() map[string]api.Method {
	return map[string]api.Method{wire.MethodFsUpload: s.upload, wire.MethodFsImage: s.image}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Dir is the directory holding every upload.
func (s *Service) Dir() string {
	return filepath.Join(s.Home, "uploads")
}

func (s *Service) upload(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		SessionID string `json:"sessionId"`
		Name      string `json:"name"`
		Data      string `json:"data"`
	}
	if err := api.Decode(raw, &p); err != nil {
		return nil, err
	}
	// The id names a directory, so only the exact id shapes are accepted:
	// no bib, separator or "..".
	managed, err := session.ValidateID(p.SessionID)
	if err != nil {
		return nil, err
	}
	if !managed {
		return nil, session.ErrNotManaged
	}
	if len(p.Data) > base64.RawURLEncoding.EncodedLen(MaxBytes) {
		return nil, tooLarge()
	}
	// The decoder skips CR and LF and, unless strict, accepts non-zero
	// trailing bits; neither belongs in a canonical encoding.
	data, err := base64.RawURLEncoding.Strict().DecodeString(p.Data)
	if err != nil || strings.ContainsAny(p.Data, "\r\n") {
		return nil, &session.InvalidParamsError{Msg: "data is not base64url without padding"}
	}
	if len(data) > MaxBytes {
		return nil, tooLarge()
	}
	sess, err := s.Sessions.Resolve(ctx, p.SessionID)
	if err != nil {
		return nil, err
	}
	path, err := s.write(sess.ID, SanitizeName(p.Name), data)
	if err != nil {
		return nil, err
	}
	return map[string]string{"path": path}, nil
}

func tooLarge() error {
	return &session.InvalidParamsError{Msg: fmt.Sprintf("upload is larger than %d bytes", MaxBytes)}
}

// write creates the file as <unix-ms>-<name> in the session's directory. A
// name taken in the same millisecond moves to the next one.
func (s *Service) write(sessionID, name string, data []byte) (string, error) {
	dir := filepath.Join(s.Dir(), sessionID)
	if err := s.prepareDir(dir); err != nil {
		return "", err
	}
	ms := s.now().UnixMilli()
	recreated := false
	for {
		path := filepath.Join(dir, strconv.FormatInt(ms, 10)+"-"+name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			ms++
			continue
		}
		// A sweep can remove the directory between prepareDir and here.
		if errors.Is(err, os.ErrNotExist) && !recreated {
			recreated = true
			if err := s.prepareDir(dir); err != nil {
				return "", err
			}
			continue
		}
		if err != nil {
			return "", fmt.Errorf("upload file: %w", err)
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			os.Remove(path)
			return "", fmt.Errorf("upload file: %w", err)
		}
		if err := f.Close(); err != nil {
			os.Remove(path)
			return "", fmt.Errorf("upload file: %w", err)
		}
		return path, nil
	}
}

// prepareDir creates the uploads root and the session directory with mode
// 0700. A symlink or a non-directory in either place is refused, so a
// planted link cannot redirect uploads or have its target's mode changed.
func (s *Service) prepareDir(dir string) error {
	for _, d := range []string{s.Dir(), dir} {
		info, err := os.Lstat(d)
		if err == nil && !info.IsDir() {
			return fmt.Errorf("upload dir: %s is not a plain directory", d)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("upload dir: %w", err)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("upload dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("upload dir: %w", err)
	}
	return nil
}

// SanitizeName reduces a client-supplied file name to [A-Za-z0-9._-], at
// most 64 bytes, keeping the extension. Directory parts are dropped and
// leading dots removed, so the result is always a plain, visible file name.
func SanitizeName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimLeft(replaceUnsafe(name), ".")
	if name == "" {
		return fallback
	}
	ext := filepath.Ext(name)
	if len(ext) > maxExtLen {
		ext = ext[:maxExtLen]
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if room := maxNameLen - len(ext); len(stem) > room {
		stem = stem[:room]
	}
	if stem == "" {
		return fallback + ext
	}
	return stem + ext
}

func replaceUnsafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Sweep removes uploads last modified more than MaxAge before now, then the
// session directories left empty.
func (s *Service) Sweep() error {
	cutoff := s.now().Add(-MaxAge)
	dirs, err := os.ReadDir(s.Dir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("upload sweep: %w", err)
	}
	var firstErr error
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if err := sweepDir(filepath.Join(s.Dir(), d.Name()), cutoff); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func sweepDir(dir string, cutoff time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("upload sweep: %w", err)
	}
	var firstErr error
	left := len(entries)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("upload sweep: %w", err)
			}
			continue
		}
		left--
	}
	if left == 0 {
		// A directory that fills again between the listing and here is
		// refused as not empty, which is the outcome wanted.
		if err := os.Remove(dir); err != nil && !errors.Is(err, syscall.ENOTEMPTY) && firstErr == nil {
			firstErr = fmt.Errorf("upload sweep: %w", err)
		}
	}
	return firstErr
}

// Run sweeps at once and then every SweepEvery until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(SweepEvery)
	defer t.Stop()
	for {
		if err := s.Sweep(); err != nil && s.Logf != nil {
			s.Logf("%v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
