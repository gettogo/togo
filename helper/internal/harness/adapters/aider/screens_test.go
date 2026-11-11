package aider

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// testdata/screens holds every confirmation aider 0.86.2 drew in a real
// run (80 columns, one at 47): each is a line-mode question the shared
// watcher offers, Yes typed as "y" Enter and No as "n" Enter. Aider's
// group options mean: (A)ll yes to this and the rest of the group, (S)kip
// all no to them, and (D)on't ask again NO, and never ask this again
// (io.confirm_ask returns False for "d"). When the watcher offers them,
// none of those may be a one-tap approve but All, and Don't ask again
// must never read as "always allow".
func TestConfirmScreens(t *testing.T) {
	files, _ := filepath.Glob("testdata/screens/real-*.txt")
	if len(files) < 10 {
		t.Fatalf("fixtures %v", files)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		p, ok := detect.Detect(string(b))
		name := filepath.Base(f)
		if !ok || p.Kind != detect.KindConfirm {
			t.Fatalf("%s: %v %+v", name, ok, p)
		}
		var yes, no int
		for _, o := range p.Options {
			keys := p.Keys[o.N]
			switch {
			case o.Label == "Yes":
				yes = o.N
				if !reflect.DeepEqual(keys, []string{"y", "Enter"}) {
					t.Errorf("%s: Yes keys %v", name, keys)
				}
			case o.Label == "No":
				no = o.N
				if o.Role != wire.RoleDeny || !reflect.DeepEqual(keys, []string{"n", "Enter"}) {
					t.Errorf("%s: No %+v %v", name, o, keys)
				}
			case strings.HasPrefix(o.Label, "Don't ask"):
				if o.Role == wire.RoleApprove || o.Role == wire.RoleApproveAlways {
					t.Errorf("%s: Don't ask again declines; role %s", name, o.Role)
				}
			case strings.HasPrefix(o.Label, "Skip"):
				if o.Role == wire.RoleApprove || o.Role == wire.RoleApproveAlways {
					t.Errorf("%s: Skip all declines; role %s", name, o.Role)
				}
			}
		}
		if yes == 0 || no == 0 {
			t.Errorf("%s: options %+v", name, p.Options)
		}
		if p.Title == "" || !strings.HasSuffix(p.Title, "?") {
			t.Errorf("%s: title %q", name, p.Title)
		}
	}
}
