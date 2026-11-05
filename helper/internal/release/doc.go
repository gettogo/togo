// Package release builds the helper for every release target in a
// reproducible way and produces the archives, checksums and build record that
// ship with a release. It is used by cmd/togo-release, a development tool that
// is never shipped.
//
// The same commit built by the same Go toolchain yields byte-identical
// binaries and archives: builds run with -trimpath, no VCS stamping, no build
// id, CGO disabled and a clean GOFLAGS, and archives carry no timestamps or
// owners other than the commit time.
package release
