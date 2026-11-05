// Package updatetest serves signed helper releases over https for tests,
// with a key generated per server so no secret key is ever stored.
package updatetest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/release"
	"github.com/gettogo/togo/helper/internal/update"
	"golang.org/x/crypto/blake2b"
)

// Server is a release host laid out like GitHub releases under /releases.
type Server struct {
	*httptest.Server
	Pub  release.PublicKey
	priv ed25519.PrivateKey

	mu     sync.Mutex
	latest string
	assets map[string][]byte // "<version>/<name>"
}

// NewServer starts an https release host that stops when the test ends.
func NewServer(t testing.TB) *Server {
	t.Helper()
	s := &Server{assets: map[string][]byte{}}
	s.Pub, s.priv = NewKey(t)
	s.Server = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// NewKey returns a fresh minisign key pair.
func NewKey(t testing.TB) (release.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	return release.PublicKey{ID: id, Key: pub}, priv
}

// Source points update at this server.
func (s *Server) Source() update.Source {
	return update.Source{BaseURL: s.URL + "/releases", Key: s.Pub, Client: s.Client()}
}

// Publish adds a release holding binary for this platform, signed by the
// server's key, and makes it the latest.
func (s *Server) Publish(t testing.TB, version string, binary []byte) {
	t.Helper()
	name := update.ArchiveName(version, runtime.GOOS, runtime.GOARCH)
	archive := Archive(t, binary)
	sums := Sums(map[string][]byte{name: archive})
	s.Put(version, name, archive)
	s.Put(version, release.SumsName, sums)
	s.Put(version, release.SumsName+".minisig", Sign(s.Pub, s.priv, sums, version))
	s.SetLatest(version)
}

// Resign signs the SHA256SUMS now stored for version with the server's
// key, naming signedAs in the trusted comment.
func (s *Server) Resign(t testing.TB, version, signedAs string) {
	t.Helper()
	sums := s.Asset(version, release.SumsName)
	if sums == nil {
		t.Fatalf("no %s for %s", release.SumsName, version)
	}
	s.Put(version, release.SumsName+".minisig", Sign(s.Pub, s.priv, sums, signedAs))
}

// Put sets one asset of a release.
func (s *Server) Put(version, name string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[version+"/"+name] = data
}

// Asset returns one asset of a release.
func (s *Server) Asset(version, name string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.assets[version+"/"+name]
}

// SetLatest chooses where /releases/latest redirects.
func (s *Server) SetLatest(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = version
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch p := r.URL.Path; {
	case p == "/releases/latest" && s.latest != "":
		http.Redirect(w, r, "/releases/tag/"+s.latest, http.StatusFound)
	case strings.HasPrefix(p, "/releases/tag/"):
		w.WriteHeader(http.StatusOK)
	case strings.HasPrefix(p, "/releases/download/"):
		data, ok := s.assets[strings.TrimPrefix(p, "/releases/download/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

// Archive packs binary as togo, next to a LICENSE, like a release archive.
func Archive(t testing.TB, binary []byte) []byte {
	t.Helper()
	return ArchiveEntries(t, map[string][]byte{"LICENSE": []byte("license\n"), "togo": binary})
}

// ArchiveEntries packs regular files in name order.
func ArchiveEntries(t testing.TB, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, name := range []string{"LICENSE", "togo"} {
		data, ok := files[name]
		if !ok {
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Size: int64(len(data)), Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Sums renders a SHA256SUMS file for the named assets.
func Sums(assets map[string][]byte) []byte {
	var b strings.Builder
	for name, data := range assets {
		sum := sha256.Sum256(data)
		b.WriteString(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	}
	return []byte(b.String())
}

// Sign makes a prehashed minisign signature of sums whose trusted comment
// names version.
func Sign(pub release.PublicKey, priv ed25519.PrivateKey, sums []byte, version string) []byte {
	h := blake2b512(sums)
	sig := ed25519.Sign(priv, h)
	trusted := "togo " + version + " SHA256SUMS"
	global := ed25519.Sign(priv, append(append([]byte{}, sig...), trusted...))
	raw := append(append([]byte("ED"), pub.ID[:]...), sig...)
	return []byte("untrusted comment: signature from minisign secret key\n" +
		base64.StdEncoding.EncodeToString(raw) + "\n" +
		"trusted comment: " + trusted + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n")
}

func blake2b512(data []byte) []byte {
	sum := blake2b.Sum512(data)
	return sum[:]
}
