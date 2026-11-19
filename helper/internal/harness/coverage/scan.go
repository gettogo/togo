package coverage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Evidence is one declaration that a test covers a cell.
type Evidence struct {
	Key  string
	Tier string
	File string // slash path relative to the scanned root
	Line int
}

// ScanEvidence finds every coverage.Covers call under root and reads its
// literal arguments. A call whose tier or keys are not string literals is
// an error: the inventory must be readable without running anything.
// Directories named testdata or vendor, hidden directories and skip (the
// coverage package itself) are not scanned.
func ScanEvidence(root string, skip ...string) ([]Evidence, error) {
	var out []Evidence
	var errs []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			for _, s := range skip {
				if s != "" && filepath.Clean(s) == filepath.Clean(path) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			errs = append(errs, err.Error())
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isCoversCall(call) {
				return true
			}
			pos := fset.Position(call.Pos())
			where := fmt.Sprintf("%s:%d", rel, pos.Line)
			if call.Ellipsis != token.NoPos {
				errs = append(errs, where+": coverage.Covers takes literal keys, not a spread slice")
				return true
			}
			if len(call.Args) < 3 {
				errs = append(errs, where+": coverage.Covers needs t, a tier and at least one key")
				return true
			}
			tier, ok := stringLit(call.Args[1])
			if !ok || !validTier(tier) {
				errs = append(errs, where+": the tier of coverage.Covers must be one of the literals live, tape, generated, unit")
				return true
			}
			for _, a := range call.Args[2:] {
				k, ok := stringLit(a)
				if !ok {
					errs = append(errs, where+": the keys of coverage.Covers must be string literals")
					continue
				}
				out = append(out, Evidence{Key: k, Tier: tier, File: rel, Line: pos.Line})
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return nil, fmt.Errorf("coverage: %s", strings.Join(errs, "\n"))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

func isCoversCall(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Covers" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "coverage"
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}
