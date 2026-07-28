package golang

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"

	envparser "github.com/PeacexF/EnvGraph/internal/parser"
)

var envFuncs = map[string]bool{
	"Getenv":    true,
	"LookupEnv": true,
}

// Parse finds os.Getenv/os.LookupEnv calls with a literal argument
func Parse(filePath string, content []byte) (envparser.Result, error) {
	var res envparser.Result

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, content, parser.SkipObjectResolution)
	if err != nil {
		return res, fmt.Errorf("parse go: %w", err)
	}

	alias := osImportName(file)
	if alias == "" {
		return res, nil
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !envFuncs[sel.Sel.Name] {
			return true
		}

		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != alias {
			return true
		}

		// A computed name such as os.Getenv(prefix+key) has no static answer
		name, ok := stringLit(call.Args[0])
		if !ok {
			return true
		}

		res.Occurrences = append(res.Occurrences, envparser.Occurrence{
			Name: name,
			Kind: envparser.KindConsumption,
			Location: envparser.Location{
				File: filePath,
				Line: fset.Position(call.Pos()).Line,
			},
		})
		return true
	})

	return res, nil
}

// osImportName returns the identifier "os" is bound to, or "" when the file does not import it. Blank and dot imports produce no os.Getenv call sites.
func osImportName(file *ast.File) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "os" {
			continue
		}
		if imp.Name == nil {
			return "os"
		}
		if imp.Name.Name == "_" || imp.Name.Name == "." {
			return ""
		}
		return imp.Name.Name
	}
	return ""
}

func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil || value == "" {
		return "", false
	}
	return value, true
}
