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
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	rel := relPath(root, path)

	fs := &FileSymbols{
		Path:        rel,
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
					EndLine:  fset.Position(d.End()).Line,
					Doc:      firstSentence(d.Doc, d.Name.Name),
				})
			}
		case *ast.GenDecl:
			syms := extractGenDecl(fset, d)
			fs.Symbols = append(fs.Symbols, syms...)
		}
	}

	// Fallback pass: if no symbols were collected (e.g. internal packages where
	// all logic lives in unexported functions), collect unexported functions and
	// methods whose body spans ≥5 lines. Exported=false ensures applySymbolBonus
	// does not inflate the file's score.
	if len(fs.Symbols) == 0 {
		fs.Symbols = collectUnexportedFallback(fset, file.Decls)
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
		Exported: true,
		Line:     fset.Position(d.Name.Pos()).Line,
		EndLine:  fset.Position(d.End()).Line,
	}

	sym.Kind = kindFor(d.Recv)
	if d.Recv != nil && len(d.Recv.List) > 0 {
		sym.Receiver = receiverString(d.Recv.List[0])
	}

	sym.Signature = funcSignature(d.Type)
	sym.ParamCount = fieldListCount(d.Type.Params)
	sym.ResultCount = fieldListCount(d.Type.Results)
	sym.Doc = firstSentence(d.Doc, d.Name.Name)
	return sym, true
}

// collectUnexportedFallback scans decls for unexported functions and methods
// whose body spans ≥5 lines and returns them as Symbols with Exported=false.
// It is called only when the main exported-symbol pass produced nothing.
func collectUnexportedFallback(fset *token.FileSet, decls []ast.Decl) []Symbol {
	var syms []Symbol
	for _, decl := range decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if isExported(fn.Name.Name) {
			continue // main loop would have caught this; skip to avoid double-add
		}
		lines := fset.Position(fn.Body.Rbrace).Line - fset.Position(fn.Body.Lbrace).Line
		if lines < 5 {
			continue
		}
		syms = append(syms, buildFallbackSymbol(fset, fn))
	}
	return syms
}

// buildFallbackSymbol constructs a Symbol for an unexported function or method.
// It mirrors extractFunc but skips the export gate and sets Exported=false.
// Callers must verify fn.Body != nil and body-line threshold before calling.
func buildFallbackSymbol(fset *token.FileSet, fn *ast.FuncDecl) Symbol {
	sym := Symbol{
		Name:        fn.Name.Name,
		Exported:    false,
		Line:        fset.Position(fn.Name.Pos()).Line,
		EndLine:     fset.Position(fn.End()).Line,
		Signature:   funcSignature(fn.Type),
		ParamCount:  fieldListCount(fn.Type.Params),
		ResultCount: fieldListCount(fn.Type.Results),
		Doc:         firstSentence(fn.Doc, fn.Name.Name),
	}
	sym.Kind = kindFor(fn.Recv)
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		sym.Receiver = receiverString(fn.Recv.List[0])
	}
	return sym
}

// kindFor returns "method" when recv is non-empty, "function" otherwise.
func kindFor(recv *ast.FieldList) string {
	if recv != nil && len(recv.List) > 0 {
		return "method"
	}
	return "function"
}

// fieldListCount counts individual items in an AST field list, accounting
// for grouped declarations like "a, b int" (which contributes 2).
func fieldListCount(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	n := 0
	for _, field := range fl.List {
		if len(field.Names) == 0 {
			n++
			continue
		}
		n += len(field.Names)
	}
	return n
}

// receiverString formats a receiver field as "*TypeName" or "TypeName".
func receiverString(field *ast.Field) string {
	if field.Type == nil {
		return ""
	}
	return typeString(field.Type)
}

// specDoc returns the doc comment for a spec within a GenDecl.
// Prefers the spec-level doc (e.g. TypeSpec.Doc or ValueSpec.Doc) if non-nil,
// otherwise falls back to the parent GenDecl doc (used for single-spec blocks).
func specDoc(parent *ast.GenDecl, spec ast.Spec) *ast.CommentGroup {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		if s.Doc != nil {
			return s.Doc
		}
	case *ast.ValueSpec:
		if s.Doc != nil {
			return s.Doc
		}
	}
	return parent.Doc
}

// firstSentence extracts a one-line subtitle from a Go doc comment.
// Strips the conventional leading identifier name ("Foo does X" -> "does X"),
// takes content up to the first '.', newline, or 60 chars, and rejects
// results shorter than 5 chars as noise.
func firstSentence(cg *ast.CommentGroup, name string) string {
	if cg == nil {
		return ""
	}
	text := cg.Text() // stdlib: strips "// " and joins lines
	text = strings.TrimSpace(text)
	// Strip conventional "Name " prefix.
	if strings.HasPrefix(text, name+" ") {
		text = text[len(name)+1:]
	}
	// Take first sentence or line.
	if i := strings.IndexAny(text, ".\n"); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimSpace(text)
	// Rune-safe truncation at 60 chars.
	if runes := []rune(text); len(runes) > 60 {
		text = string(runes[:60])
	}
	if len(text) < 5 {
		return ""
	}
	return text
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
			var memberCount int
			switch t := ts.Type.(type) {
			case *ast.StructType:
				kind = "struct"
				signature = structFields(t)
				memberCount = fieldListCount(t.Fields)
			case *ast.InterfaceType:
				kind = "interface"
				signature = interfaceMethods(t)
				memberCount = fieldListCount(t.Methods)
			}
			syms = append(syms, Symbol{Name: ts.Name.Name, Kind: kind, Exported: true, Signature: signature, Line: fset.Position(ts.Name.Pos()).Line, EndLine: fset.Position(ts.End()).Line, ParamCount: memberCount, Doc: firstSentence(specDoc(d, ts), ts.Name.Name)})
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
					syms = append(syms, Symbol{Name: name.Name, Kind: kind, Exported: true, Line: fset.Position(name.Pos()).Line, EndLine: fset.Position(vs.End()).Line, Doc: firstSentence(specDoc(d, vs), name.Name)})
				}
			}
		}
	}
	return syms
}
