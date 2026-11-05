// Package update replaces the running togo binary with a signed release.
// Nothing is written next to the binary until the checksum list verifies
// against the pinned key and names the requested version, and the archive
// matches its checksum. The new binary then replaces the old one with a
// single rename in the same directory, and the old one is kept beside it.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gettogo/togo/helper/internal/release"
)

// BaseURL is where releases are published. Release builds may set it with
// -ldflags "-X github.com/gettogo/togo/helper/internal/update.BaseURL=...";
// it is never read from the environment.
var BaseURL = "https://github.com/gettogo/togo/releases"

// Source is where Check and Apply fetch releases from and the key their
// checksum lists must be signed with.
type Source struct {
	// BaseURL holds latest (redirecting to tag/<version>) and
	// download/<version>/<asset>. It must be https.
	BaseURL string
	Key     release.PublicKey
	// Client fetches over https; nil means a client with a timeout.
	Client *http.Client
}

// Download limits. A release asset larger than these is refused before it
// is read in full.
const (
	maxSums    = 64 << 10
	maxSig     = 4 << 10
	maxArchive = 128 << 20
	maxBinary  = 256 << 20
)

// PreviousSuffix names the copy of the replaced binary kept for rollback.
const PreviousSuffix = ".previous"

var (
	ErrNotHTTPS        = errors.New("update: release URLs must use https")
	ErrBadVersion      = errors.New("update: not a release version")
	ErrChecksum        = errors.New("update: archive does not match its signed checksum")
	ErrMissingPlatform = errors.New("update: the release has no archive for this platform")
)

// Check returns the latest released version and whether it is newer than
// current. A current version that is not a release version, such as dev,
// counts as older than every release.
func Check(ctx context.Context, s Source, current string) (latest string, newer bool, err error) {
	latest, err = latestVersion(ctx, s)
	if err != nil {
		return "", false, err
	}
	return latest, Compare(latest, current) > 0, nil
}

// Apply downloads version, verifies it, and replaces exe with its togo
// binary. Any failure leaves exe as it was and removes what was written.
func Apply(ctx context.Context, s Source, version, exe string) error {
	if !ValidVersion(version) {
		return fmt.Errorf("%w: %q", ErrBadVersion, version)
	}
	if !filepath.IsAbs(exe) {
		return fmt.Errorf("update: binary path %q is not absolute", exe)
	}
	sums, err := fetch(ctx, s, "download/"+version+"/"+release.SumsName, maxSums)
	if err != nil {
		return err
	}
	sig, err := fetch(ctx, s, "download/"+version+"/"+release.SumsName+".minisig", maxSig)
	if err != nil {
		return err
	}
	if err := release.VerifySums(s.Key, sums, sig, version); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	table, err := release.ParseSums(sums)
	if err != nil {
		return fmt.Errorf("update: checksum list: %w", err)
	}
	name := ArchiveName(version, runtime.GOOS, runtime.GOARCH)
	want, ok := table[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrMissingPlatform, name)
	}
	archive, err := fetch(ctx, s, "download/"+version+"/"+name, maxArchive)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
		return fmt.Errorf("%w: %s", ErrChecksum, name)
	}
	return replace(exe, archive)
}

// ArchiveName is the release archive for one platform.
func ArchiveName(version, goos, goarch string) string {
	return fmt.Sprintf("togo_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, goarch)
}

// replace writes the archive's togo to a temporary file beside exe, keeps
// exe as exe.previous, and renames the new binary over exe.
func replace(exe string, archive []byte) (err error) {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".togo-update-*")
	if err != nil {
		return fmt.Errorf("update: cannot write next to %s: %w", exe, err)
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if err := extract(archive, tmp); err != nil {
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	finish, err := keepPrevious(exe)
	if err != nil {
		return err
	}
	if err := rename(tmp.Name(), exe); err != nil {
		finish(false)
		return fmt.Errorf("update: replace %s: %w", exe, err)
	}
	finish(true)
	return nil
}

// rename is os.Rename; tests replace it to make the final step fail.
var rename = os.Rename

// keepPrevious leaves the current binary at exe.previous for rollback,
// as a hard link when the file system allows and a copy otherwise. An
// older exe.previous is set aside until the replacement is settled:
// finish(true) drops it, finish(false) removes the new exe.previous and
// puts the older one back.
func keepPrevious(exe string) (finish func(replaced bool), err error) {
	prev := exe + PreviousSuffix
	aside := ""
	if _, err := os.Lstat(prev); err == nil {
		aside = filepath.Join(filepath.Dir(exe), fmt.Sprintf(".togo-previous-%d", time.Now().UnixNano()))
		if err := os.Rename(prev, aside); err != nil {
			return nil, fmt.Errorf("update: keep previous binary: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("update: keep previous binary: %w", err)
	}
	undo := func() {
		os.Remove(prev)
		if aside != "" {
			os.Rename(aside, prev)
		}
	}
	if err := os.Link(exe, prev); err != nil {
		data, err := os.ReadFile(exe)
		if err == nil {
			err = os.WriteFile(prev, data, 0o755)
		}
		if err != nil {
			undo()
			return nil, fmt.Errorf("update: keep previous binary: %w", err)
		}
	}
	return func(replaced bool) {
		switch {
		case !replaced:
			undo()
		case aside != "":
			os.Remove(aside)
		}
	}, nil
}

// extract copies the archive's one regular file named togo into w.
func extract(archive []byte, w io.Writer) error {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("update: archive: %w", err)
	}
	tr := tar.NewReader(zr)
	found := false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("update: archive: %w", err)
		}
		if h.Name != "togo" {
			continue
		}
		if found || h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > maxBinary {
			return errors.New("update: archive: togo is not a single regular file")
		}
		found = true
		if _, err := io.CopyN(w, tr, h.Size); err != nil {
			return fmt.Errorf("update: archive: %w", err)
		}
	}
	if !found {
		return errors.New("update: archive has no togo binary")
	}
	return nil
}

// latestVersion follows BaseURL/latest to its tag/<version> page.
func latestVersion(ctx context.Context, s Source) (string, error) {
	resp, err := get(ctx, s, "latest")
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	dir, v := path.Split(resp.Request.URL.Path)
	if path.Base(path.Clean(dir)) != "tag" || !ValidVersion(v) {
		return "", fmt.Errorf("update: latest release did not lead to a version: %s", resp.Request.URL.Redacted())
	}
	return v, nil
}

// fetch downloads one asset of at most limit bytes.
func fetch(ctx context.Context, s Source, rel string, limit int64) ([]byte, error) {
	resp, err := get(ctx, s, rel)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("update: %s is larger than %d bytes", path.Base(rel), limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("update: download %s: %w", path.Base(rel), err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("update: %s is larger than %d bytes", path.Base(rel), limit)
	}
	if resp.ContentLength >= 0 && int64(len(data)) != resp.ContentLength {
		return nil, fmt.Errorf("update: download %s: truncated", path.Base(rel))
	}
	return data, nil
}

func get(ctx context.Context, s Source, rel string) (*http.Response, error) {
	base, err := url.Parse(strings.TrimSuffix(s.BaseURL, "/") + "/")
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, ErrNotHTTPS
	}
	u := base.JoinPath(rel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	resp, err := httpsOnly(s.Client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("update: %s: %s", u.Redacted(), resp.Status)
	}
	return resp, nil
}

// httpsOnly copies c, or a default client, and refuses redirects that
// leave https.
func httpsOnly(c *http.Client) *http.Client {
	out := http.Client{Timeout: 5 * time.Minute}
	if c != nil {
		out = *c
	}
	out.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return ErrNotHTTPS
		}
		if len(via) >= 10 {
			return errors.New("update: too many redirects")
		}
		return nil
	}
	return &out
}
