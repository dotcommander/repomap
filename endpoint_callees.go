package repomap

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// handlerCallees returns the direct (depth-1) callee names lexically present in
// the handler body spanning [startLine, endLine] of the Go file at path. It
// parses the file with go/parser and records the callee text of every
// *ast.CallExpr whose position falls inside the line range, deduped in
// first-seen order. Depth 1 only: no cross-file or transitive resolution.
func handlerCallees(path string, startLine, endLine int) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var callees []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		pos := fset.Position(call.Pos())
		if pos.Line < startLine || pos.Line > endLine {
			return true
		}
		if name := calleeName(call.Fun); name != "" {
			callees = append(callees, name)
		}
		return true
	})
	return dedupeStrings(callees), nil
}

// calleeName renders a call target as lexical text: a bare identifier
// ("parseID") or a selector chain ("db.FindUser"). Forms we do not name (an
// immediately-invoked func literal, an index expression) return "".
func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if x := calleeName(f.X); x != "" {
			return x + "." + f.Sel.Name
		}
		return f.Sel.Name
	default:
		return ""
	}
}
