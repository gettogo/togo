package release

import (
	"os"
	"strings"
	"testing"
)

func TestBuildInfoStableJSON(t *testing.T) {
	b := BuildInfo{
		Version: "v1.2.3", Commit: "abc123", GoVersion: "go1.26.0", Dirty: true, SourceDateEpoch: 1700000000,
		Binaries: map[string]string{"linux/arm64": "bb", "darwin/arm64": "aa", "linux/amd64": "cc"},
	}
	var first []byte
	for i := 0; i < 5; i++ {
		path, err := WriteBuildInfo(t.TempDir(), b)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(path, "BUILDINFO.json") {
			t.Errorf("path = %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = data
		} else if string(first) != string(data) {
			t.Fatal("output differs between writes")
		}
	}
	want := `{
  "binaries": {
    "darwin/arm64": "aa",
    "linux/amd64": "cc",
    "linux/arm64": "bb"
  },
  "commit": "abc123",
  "dirty": true,
  "goVersion": "go1.26.0",
  "sourceDateEpoch": 1700000000,
  "version": "v1.2.3"
}
`
	if string(first) != want {
		t.Errorf("got\n%s\nwant\n%s", first, want)
	}
}
