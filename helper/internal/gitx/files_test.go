package gitx_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/gitx"
	"github.com/gettogo/togo/helper/internal/rpc"
)

func TestFilesStayInsideOneFrame(t *testing.T) {
	dir := newRepo(t)
	long := strings.Repeat("d", 200)
	for i := 0; i < 1500; i++ {
		write(t, dir, filepath.Join(long, long, long, strings.Repeat("f", 190)+fmt.Sprintf("%05d", i)), "")
	}
	var res gitx.Files
	if err := call(t, service(dir), "git.files", `{`+sid+`}`, &res); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": res})
	if len(b) > rpc.MaxFrame {
		t.Errorf("response is %d bytes, frame limit %d", len(b), rpc.MaxFrame)
	}
	if !res.Truncated || len(res.Paths) == 0 || len(res.Paths) >= 1500 {
		t.Errorf("truncated=%v with %d paths", res.Truncated, len(res.Paths))
	}
}
