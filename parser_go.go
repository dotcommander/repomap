package repomap

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ParseGoFile extracts exported symbols from a Go source file.
// path is absolute, root is the project root for relative path calculation.
func ParseGoFile(path, root string) (*FileSymbols, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	relPath, err := filepath.Rel(root, path)
	if err != nil {
		relPath = path
	}

	fs := &FileSymbols{
		Path:        relPath,
		Language:    "go",
		Package:     file.Name.Name,
		ImportPath:  resolveImportPath(path, root),
		ParseMethod: "ast",
	}

	// Collect imports.
	for _, imp := range file.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		fs.Imports = append(fs.Imports, impPath)
	}

	// Walk top-level declarations.
	// For package main, also capture unexported main() and init() as entry points.
	isMain := fs.Package == "main"
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if sym, ok := extractFunc(fset, d); ok {
				fs.Symbols = append(fs.Symbols, sym)
			} else if isMain && d.Recv == nil &&
				(d.Name.Name == "main" || d.Name.Name == "init") {
				fs.Symbols = append(fs.Symbols, Symbol{
					Name:     d.Name.Name,
					Kind:     "function",
					Exported: false,
					Line:     fset.Position(d.Name.Pos()).Line,
				})
			}
		case *ast.GenDecl:
			syms := extractGenDecl(fset, d)
			fs.Symbols = append(fs.Symbols, syms...)
		}
	}

	return fs, nil
}

// moduleInfo caches the result of go.mod discovery for a project root.
type moduleInfo struct {
	name   string // module name from go.mod
	modDir string // directory containing go.mod
}

// cachedModules caches go.mod lookups keyed by project root.
// Safe for concurrent use because parseFiles runs in a single errgroup
// and all goroutines share the same root, so the cache is populated
// at most once before concurrent reads.
var cachedModules sync.Map // map[string]*moduleInfo

// resolveImportPath computes the Go import path for absPath relative to
// the module root at root. Caches go.mod discovery per root.
func resolveImportPath(absPath, root string) string {
	mi := getModuleInfo(root)
	if mi == nil {
		return ""
	}

	rel, err := filepath.Rel(mi.modDir, filepath.Dir(absPath))
	if err != nil {
		return ""
	}

	if rel == "." {
		return mi.name
	}
	return mi.name + "/" + filepath.ToSlash(rel)
}

// getModuleInfo returns cached module info for the given root, or discovers it.
func getModuleInfo(root string) *moduleInfo {
	if v, ok := cachedModules.Load(root); ok {
		return v.(*moduleInfo)
	}

	modPath := findGoMod(root)
	if modPath == "" {
		cachedModules.Store(root, (*moduleInfo)(nil))
		return nil
	}

	data, err := os.ReadFile(modPath)
	if err != nil {
		cachedModules.Store(root, (*moduleInfo)(nil))
		return nil
	}

	name := parseModuleName(string(data))
	if name == "" {
		cachedModules.Store(root, (*moduleInfo)(nil))
		return nil
	}

	mi := &moduleInfo{name: name, modDir: filepath.Dir(modPath)}
	cachedModules.Store(root, mi)
	return mi
}

// findGoMod walks up from root looking for go.mod.
func findGoMod(root string) string {
	dir := root
	for {
		candidate := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// isExported reports whether name begins with an uppercase ASCII letter,
// meaning it is an exported Go identifier.
func isExported(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

// parseModuleName extracts the module name from go.mod content.
func parseModuleName(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if name, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

// extractFunc extracts a Symbol from a function or method declaration.
// Returns (Symbol, false) if the function is unexported.
func extractFunc(fset *token.FileSet, d *ast.FuncDecl) (Symbol, bool) {
	if !isExported(d.Name.Name) {
		return Symbol{}, false
	}

	sym := Symbol{
		Name:     d.Name.Name,
		Kind:     "function",
		Exported: true,
		Line:     fset.Position(d.Name.Pos()).Line,
	}

	if d.Recv != nil && len(d.Recv.List) > 0 {
		sym.Kind = "method"
		sym.Receiver = receiverString(d.Recv.List[0])
	}

	sym.Signature = funcSignature(d.Type)
	return sym, true
}

// receiverString formats a receiver field as "*TypeName" or "TypeName".
func receiverString(field *ast.Field) string {
	if field.Type == nil {
		return ""
	}
	return typeString(field.Type)
}

// extractGenDecl extracts symbols from a general declaration (type, const, var).
func extractGenDecl(fset *token.FileSet, d *ast.GenDecl) []Symbol {
	var syms []Symbol
	switch d.Tok {
	case token.TYPE:
		for _, spec := range d.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !isExported(ts.Name.Name) {
				continue
			}
			kind := "type"
			var signature string
			switch t := ts.Type.(type) {
			case *ast.StructType:
				kind = "struct"
				signature = structFields(t)
			case *ast.InterfaceType:
				kind = "interface"
				signature = interfaceMethods(t)
			}
			syms = append(syms, Symbol{Name: ts.Name.Name, Kind: kind, Exported: true, Signature: signature, Line: fset.Position(ts.Name.Pos()).Line})
		}
	case token.CONST, token.VAR:
		kind := "constant"
		if d.Tok == token.VAR {
			kind = "variable"
		}
		for _, spec := range d.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				if isExported(name.Name) {
					syms = append(syms, Symbol{Name: name.Name, Kind: kind, Exported: true, Line: fset.Position(name.Pos()).Line})
				}
			}
		}
	}
	return syms
}
