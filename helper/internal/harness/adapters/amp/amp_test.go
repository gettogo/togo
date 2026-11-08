package amp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/engine"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "amp" || a.Feeds["hooks"].Mapper != MapperHooks || a.Feeds["hooks"].Stability != spec.StableUnverified {
		t.Fatalf("adapter %+v", a.Feeds)
	}
	var logs []string
	engine.New(engine.Config{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
	for _, l := range logs {
		if strings.Contains(l, "amp") {
			t.Fatal(l)
		}
	}
}
