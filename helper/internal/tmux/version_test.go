package tmux

import "testing"

func TestVersionParses(t *testing.T) {
	cases := []struct {
		in      string
		major   int
		minor   int
		wantRaw string
		wantErr bool
	}{
		{in: "tmux 3.6a", major: 3, minor: 6, wantRaw: "tmux 3.6a"},
		{in: "tmux next-3.7", major: 3, minor: 7, wantRaw: "tmux next-3.7"},
		{in: "tmux openbsd-7.5", major: 7, minor: 5, wantRaw: "tmux openbsd-7.5"},
		{in: "tmux", wantErr: true},
	}
	for _, tc := range cases {
		major, minor, raw, err := parseVersion(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseVersion(%q) returned err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if tc.wantErr {
			continue
		}
		if major != tc.major || minor != tc.minor || raw != tc.wantRaw {
			t.Errorf("parseVersion(%q) = %d, %d, %q; want %d, %d, %q",
				tc.in, major, minor, raw, tc.major, tc.minor, tc.wantRaw)
		}
	}
}
