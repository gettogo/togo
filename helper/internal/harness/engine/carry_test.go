package engine

import (
	"reflect"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// A resume from the app restarted omp as "omp --resume <id>" alone, so the
// session lost the profile, tools and folders it was started with. Listed
// options are carried; a prompt, a conversation pick and one-off flags of
// the old command line never are.
func TestRelaunchCarriesListedOptionsOnly(t *testing.T) {
	c := &spec.Carry{Values: []string{"--profile", "--tools", "--add-dir", "--model", "-e"}, Flags: []string{"--no-skills", "--yolo"}}
	next := []string{"omp", "--resume", "abc"}
	cases := []struct {
		name    string
		current []string
		want    []string
	}{
		{"values and flags", []string{"omp", "--profile", "work", "--no-skills", "--tools", "bash,read"}, []string{"--profile", "work", "--no-skills", "--tools", "bash,read"}},
		{"joined value", []string{"omp", "--profile=work"}, []string{"--profile=work"}},
		{"repeated value", []string{"omp", "--add-dir", "/a", "--add-dir", "/b"}, []string{"--add-dir", "/a", "--add-dir", "/b"}},
		{"old conversation pick and prompt dropped", []string{"omp", "--continue", "--resume", "old", "--print", "fix the bug", "--yolo"}, []string{"--yolo"}},
		{"prompt after -- is never read", []string{"omp", "--no-skills", "--", "--yolo", "--profile", "x"}, []string{"--no-skills"}},
		{"a value that looks like a flag stays a value", []string{"omp", "--model", "--yolo"}, []string{"--model", "--yolo"}},
		{"unknown options dropped", []string{"omp", "--mode", "rpc", "--goal", "ship it"}, nil},
		{"dangling value option", []string{"omp", "--profile"}, nil},
	}
	for _, tc := range cases {
		if got := carried(tc.current, c, next); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: carried = %q, want %q", tc.name, got, tc.want)
		}
	}
	// The new command line wins over the old one.
	got := carried([]string{"omp", "--model", "old", "--yolo"}, c, []string{"omp", "--resume", "abc", "--model", "new"})
	if want := []string{"--yolo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("carried = %q, want %q", got, want)
	}
	if got := carried([]string{"omp", "--yolo"}, nil, next); got != nil {
		t.Errorf("no carry list: %q", got)
	}
}
