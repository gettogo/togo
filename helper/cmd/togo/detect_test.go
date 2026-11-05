package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

func TestDetectCommandMatchesFixture(t *testing.T) {
	dir := filepath.Join("..", "..", "internal", "detect", "testdata", "claude")
	screen, err := os.ReadFile(filepath.Join(dir, "bash-permission.txt"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "bash-permission.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Kind    string              `json:"kind"`
		Title   string              `json:"title"`
		Options []wire.Option       `json:"options"`
		Keys    map[string][]string `json:"keys"`
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"detect"}, nil, bytes.NewReader(screen), &out, &errOut)
	if code != exitOK {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	var got detectOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output %q: %v", out.String(), err)
	}
	if !got.Match || got.Kind != want.Kind || got.Title != want.Title {
		t.Errorf("match, kind, title = %v, %q, %q", got.Match, got.Kind, got.Title)
	}
	if !reflect.DeepEqual(got.Options, want.Options) {
		t.Errorf("options = %+v, want %+v", got.Options, want.Options)
	}
	if !reflect.DeepEqual(got.Keys, want.Keys) {
		t.Errorf("keys = %v, want %v", got.Keys, want.Keys)
	}
	if got.Detail == nil || !strings.Contains(*got.Detail, "rm -rf ./build && make") || got.Fingerprint == "" {
		t.Errorf("detail %v or fingerprint %q missing", got.Detail, got.Fingerprint)
	}
}

func TestDetectCommandNoMatchExit1(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"detect"}, nil, strings.NewReader("$ ls\nREADME.md\n$ \n"), &out, &errOut)
	if code != exitError {
		t.Fatalf("code %d, want %d", code, exitError)
	}
	var got detectOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Match {
		t.Errorf("output %q, err %v", out.String(), err)
	}
}

func TestDetectCommandHiddenFromUsage(t *testing.T) {
	if strings.Contains(usage, "detect") {
		t.Error("detect is listed in the usage text")
	}
}
