package onesie

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestOnesie(t *testing.T) {
	t.Parallel()

	t.Run("should write the onesie prefix only inside an Error method", func(t *testing.T) {
		t.Parallel()

		entries, err := os.ReadDir(".")
		if err != nil {
			t.Fatalf("reading the package: %v", err)
		}

		fset := token.NewFileSet()

		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}

			file, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", name, err)
			}

			for _, decl := range file.Decls {
				if fn, isFunc := decl.(*ast.FuncDecl); isFunc && fn.Recv != nil && fn.Name.Name == "Error" {
					continue
				}

				ast.Inspect(decl, func(node ast.Node) bool {
					lit, isLit := node.(*ast.BasicLit)
					if isLit && lit.Kind == token.STRING && strings.Contains(lit.Value, "onesie: ") {
						t.Errorf("%s: %s is a prefixed literal outside an Error method", fset.Position(lit.Pos()), lit.Value)
					}

					return true
				})
			}
		}
	})
}
