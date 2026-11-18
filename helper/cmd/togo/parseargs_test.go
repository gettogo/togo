package main

import (
	"slices"
	"testing"
)

func TestParseArgsDoubleDashEndsFlags(t *testing.T) {
	fs := newFlagSet("t")
	strict := fs.Bool("strict", false, "")
	got, err := parseArgs(fs, []string{"done", "--", "-fast", "--strict"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"done", "-fast", "--strict"}; !slices.Equal(got, want) {
		t.Errorf("positional = %q, want %q", got, want)
	}
	if *strict {
		t.Error("a flag after -- was parsed")
	}
}

func TestParseArgsDoubleDashAsFlagValue(t *testing.T) {
	fs := newFlagSet("t")
	title := fs.String("title", "", "")
	got, err := parseArgs(fs, []string{"--title", "--", "-x"})
	if err == nil {
		t.Fatalf("got %q, want -x rejected as an unknown flag", got)
	}
	if *title != "--" {
		t.Errorf("title = %q, want the -- value", *title)
	}
}

func TestParseTextArgsKeepsDashWordsAsText(t *testing.T) {
	fs := newFlagSet("t")
	strict := fs.Bool("strict", false, "")
	title := fs.String("title", "", "")
	got, err := parseTextArgs(fs, []string{"-3 tests failed", "--title", "CI", "- ship it?", "-O3=fast", "--strict"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-3 tests failed", "- ship it?", "-O3=fast"}; !slices.Equal(got, want) {
		t.Errorf("positional = %q, want %q", got, want)
	}
	if !*strict || *title != "CI" {
		t.Errorf("flags lost: strict=%v title=%q", *strict, *title)
	}
}

func TestParseArgsStillRejectsUnknownFlags(t *testing.T) {
	fs := newFlagSet("t")
	if _, err := parseArgs(fs, []string{"x", "--nope"}); err == nil {
		t.Error("unknown flag accepted")
	}
}
