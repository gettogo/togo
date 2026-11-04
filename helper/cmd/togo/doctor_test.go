package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/doctor"
)

func doctorEnv(t *testing.T, tmuxVersion string) doctor.Env {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return doctor.Env{
		Path: bin, GOOS: "linux",
		Run: func(context.Context, string, ...string) (string, error) { return tmuxVersion, nil },
	}
}

func TestDoctorOldTmuxExitsOne(t *testing.T) {
	var buf bytes.Buffer
	if code := runDoctor(nil, doctorEnv(t, "tmux 2.9"), &buf); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	first, _, _ := strings.Cut(buf.String(), "\n")
	if !strings.HasPrefix(first, "tmux ") || !strings.Contains(first, "warn") {
		t.Errorf("tmux line = %q", first)
	}
}

func TestDoctorGoodTmuxExitsZero(t *testing.T) {
	var buf bytes.Buffer
	if code := runDoctor(nil, doctorEnv(t, "tmux 3.6a"), &buf); code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, &buf)
	}
}

func TestDoctorJSON(t *testing.T) {
	var buf bytes.Buffer
	runDoctor([]string{"--json"}, doctorEnv(t, "tmux 3.6a"), &buf)
	var got struct {
		Checks []map[string]any `json:"checks"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, &buf)
	}
	valid := map[string]bool{"ok": true, "missing": true, "warn": true, "note": true}
	if len(got.Checks) == 0 {
		t.Fatal("no checks")
	}
	for _, c := range got.Checks {
		for _, k := range []string{"name", "status", "detail"} {
			if s, _ := c[k].(string); s == "" {
				t.Errorf("check %v lacks %s", c, k)
			}
		}
		if !valid[c["status"].(string)] {
			t.Errorf("bad status in %v", c)
		}
	}
}

func TestDoctorRejectsArguments(t *testing.T) {
	var buf bytes.Buffer
	if code := runDoctor([]string{"extra"}, doctorEnv(t, "tmux 3.6a"), &buf); code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}
