package generic

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Every profile type in sessiongen is in its registry, so the scenarios
// here run for it. A profile type is one with the Profile methods.
func TestEveryProfileIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, n := range sessiongen.Names() {
		p := sessiongen.For(n)
		if p.Name() != n {
			t.Errorf("profile registered as %q calls itself %q", n, p.Name())
		}
		registered[reflect.TypeOf(p).Elem().Name()] = true
	}
	files, _ := filepath.Glob(filepath.Join("..", "..", "sessiongen", "*.go"))
	methods := map[string]map[string]bool{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			id, ok := star.X.(*ast.Ident)
			if !ok {
				continue
			}
			if methods[id.Name] == nil {
				methods[id.Name] = map[string]bool{}
			}
			methods[id.Name][fn.Name.Name] = true
		}
	}
	if len(methods) == 0 {
		t.Fatal("no sessiongen source found")
	}
	for typ, m := range methods {
		if m["Name"] && m["Open"] && m["Turn"] && !registered[typ] {
			t.Errorf("profile %s is not in sessiongen's registry (registry.go): the generic scenarios do not run for it", typ)
		}
	}
}
