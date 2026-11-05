package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/gettogo/togo/helper/internal/release"
)

const formulaUsage = "usage: togo-release formula --version vX.Y.Z --sums SHA256SUMS --base-url <releases url> [--template file]\n"

type formulaAsset struct{ URL, SHA256 string }

type formulaData struct {
	Version                                          string
	DarwinArm64, DarwinAmd64, LinuxArm64, LinuxAmd64 formulaAsset
}

// runFormula renders the Homebrew formula from the release checksums. The
// output goes to stdout only when every target resolved.
func runFormula(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("formula", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.String("version", "", "release version, vX.Y.Z")
	sums := fs.String("sums", "", "path to SHA256SUMS")
	baseURL := fs.String("base-url", "", "https URL of the release's download directory")
	tmpl := fs.String("template", "scripts/release/togo.rb.tmpl", "formula template")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *sums == "" {
		fmt.Fprint(stderr, formulaUsage)
		return 2
	}
	if !versionPattern.MatchString(*version) || !strings.HasPrefix(*baseURL, "https://") {
		fmt.Fprintf(stderr, "togo-release: formula needs --version vX.Y.Z and an https --base-url\n%s", formulaUsage)
		return 2
	}
	out, err := renderFormula(*version, *sums, *baseURL, *tmpl)
	if err != nil {
		fmt.Fprintf(stderr, "togo-release: %v\n", err)
		return 1
	}
	_, _ = stdout.Write(out)
	return 0
}

func renderFormula(version, sumsPath, baseURL, tmplPath string) ([]byte, error) {
	data, err := os.ReadFile(sumsPath)
	if err != nil {
		return nil, err
	}
	entries, err := sumsEntries(data)
	if err != nil {
		return nil, err
	}
	hashes := map[string]string{}
	for _, e := range entries {
		hashes[e.name] = fmt.Sprintf("%x", e.sum)
	}
	c := release.Config{Version: version}
	base := strings.TrimRight(baseURL, "/")
	asset := func(os, arch string) (formulaAsset, error) {
		name := c.ArchiveName(release.Target{OS: os, Arch: arch})
		sum, ok := hashes[name]
		if !ok {
			return formulaAsset{}, fmt.Errorf("SHA256SUMS does not list %s", name)
		}
		return formulaAsset{URL: base + "/" + name, SHA256: sum}, nil
	}
	d := formulaData{Version: strings.TrimPrefix(version, "v")}
	for _, t := range []struct {
		dst  *formulaAsset
		os   string
		arch string
	}{
		{&d.DarwinArm64, "darwin", "arm64"}, {&d.DarwinAmd64, "darwin", "amd64"},
		{&d.LinuxArm64, "linux", "arm64"}, {&d.LinuxAmd64, "linux", "amd64"},
	} {
		if *t.dst, err = asset(t.os, t.arch); err != nil {
			return nil, err
		}
	}
	text, err := os.ReadFile(tmplPath)
	if err != nil {
		return nil, err
	}
	t, err := template.New("formula").Option("missingkey=error").Parse(string(text))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
