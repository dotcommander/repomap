// Package goanalysis owns module-aware Go semantic loading for repomap.
package goanalysis

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
	"golang.org/x/tools/go/types/typeutil"
)

// Options describes the active Go build context used for a repository load.
type Options struct {
	Root         string
	Environment  []string
	BuildFlags   []string
	IncludeCalls bool
	IncludeTests bool
}

// Diagnostic is a package loader or type-checker diagnostic. Diagnostics do
// not discard successfully loaded packages.
type Diagnostic struct {
	PackagePath string
	Position    string
	Message     string
}

// File is an active compiled Go file and its package ownership.
type File struct {
	Path        string
	PackageName string
	PackagePath string
	Syntax      *ast.File
	FileSet     *token.FileSet
	Types       *types.Package
	TypesInfo   *types.Info
}

// EdgeKind identifies a typed semantic relationship.
type EdgeKind string

const (
	EdgeDefinition     EdgeKind = "definition"
	EdgeReference      EdgeKind = "reference"
	EdgeImport         EdgeKind = "import"
	EdgeCall           EdgeKind = "call"
	EdgeImplementation EdgeKind = "implementation"
	EdgeSelection      EdgeKind = "selection"
)

// Edge is an internal, qualified semantic relationship.
type Edge struct {
	Kind EdgeKind
	From string
	To   string
	File string
	Line int
}

// CallEdge is a source-backed caller to callee relationship from CHA.
type CallEdge struct {
	CallerFile, CallerSymbol string
	CallerLine               int
	CalleeFile, CalleeSymbol string
	CalleeReceiver           string
}

// Implementation records a concrete named type satisfying an interface.
type Implementation struct {
	TypeFile      string
	TypeName      string
	InterfacePath string
	InterfaceName string
}

// Result is the canonical semantic graph for one repository load.
type Result struct {
	Files           map[string]File
	Diagnostics     []Diagnostic
	Edges           []Edge
	Calls           []CallEdge
	Implementations []Implementation
}

// Analyze loads and analyzes all packages beneath Root exactly once.
func Analyze(ctx context.Context, opts Options) (*Result, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve analysis root: %w", err)
	}
	mode := packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
		packages.NeedImports | packages.NeedDeps | packages.NeedModule |
		packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedTypesSizes
	cfg := &packages.Config{
		Context:    ctx,
		Dir:        root,
		Mode:       mode,
		Env:        opts.Environment,
		BuildFlags: opts.BuildFlags,
		Tests:      opts.IncludeTests,
	}
	pkgs, err := packages.Load(cfg, modulePatterns(root)...)
	if err != nil {
		return nil, fmt.Errorf("load Go packages: %w", err)
	}
	result := &Result{Files: make(map[string]File)}
	usable := make([]*packages.Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		for _, pkgErr := range pkg.Errors {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				PackagePath: pkg.PkgPath, Position: pkgErr.Pos, Message: pkgErr.Msg,
			})
		}
		if pkg.Types == nil || pkg.TypesInfo == nil || len(pkg.Syntax) == 0 {
			continue
		}
		usable = append(usable, pkg)
		for i, syntax := range pkg.Syntax {
			if i >= len(pkg.CompiledGoFiles) {
				continue
			}
			rel, relErr := filepath.Rel(root, pkg.CompiledGoFiles[i])
			if relErr != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			result.Files[filepath.ToSlash(rel)] = File{
				Path: filepath.ToSlash(rel), PackageName: pkg.Name, PackagePath: pkg.PkgPath,
				Syntax: syntax, FileSet: pkg.Fset, Types: pkg.Types, TypesInfo: pkg.TypesInfo,
			}
		}
		result.Edges = append(result.Edges, semanticEdges(root, pkg)...)
	}
	if len(result.Files) == 0 {
		return nil, errors.New("no usable Go packages")
	}
	result.Implementations = findImplementations(root, usable)
	if opts.IncludeCalls {
		result.Calls = buildCalls(root, usable)
	}
	for _, implementation := range result.Implementations {
		result.Edges = append(result.Edges, Edge{
			Kind: EdgeImplementation,
			From: implementation.TypeFile + "|" + implementation.TypeName,
			To:   implementation.InterfacePath + "|" + implementation.InterfaceName,
			File: implementation.TypeFile,
		})
	}
	for _, call := range result.Calls {
		result.Edges = append(result.Edges, Edge{
			Kind: EdgeCall,
			From: call.CallerFile + "|" + call.CallerSymbol,
			To:   call.CalleeFile + "|" + call.CalleeSymbol,
			File: call.CallerFile,
			Line: call.CallerLine,
		})
	}
	slices.SortFunc(result.Diagnostics, func(a, b Diagnostic) int {
		return strings.Compare(a.PackagePath+"\x00"+a.Position+"\x00"+a.Message, b.PackagePath+"\x00"+b.Position+"\x00"+b.Message)
	})
	slices.SortFunc(result.Edges, compareEdge)
	slices.SortFunc(result.Calls, compareCall)
	slices.SortFunc(result.Implementations, compareImplementation)
	return result, nil
}

func semanticEdges(root string, pkg *packages.Package) []Edge {
	edges := make([]Edge, 0, len(pkg.TypesInfo.Defs)+len(pkg.TypesInfo.Uses)+len(pkg.TypesInfo.Selections))
	for id, obj := range pkg.TypesInfo.Defs {
		if obj != nil {
			edges = append(edges, objectEdge(root, pkg.Fset, EdgeDefinition, qualifiedObject(obj), qualifiedObject(obj), id.Pos()))
		}
	}
	for id, obj := range pkg.TypesInfo.Uses {
		if obj != nil {
			edges = append(edges, objectEdge(root, pkg.Fset, EdgeReference, positionID(pkg.Fset, id.Pos()), qualifiedObject(obj), id.Pos()))
		}
	}
	for sel, selection := range pkg.TypesInfo.Selections {
		edges = append(edges, objectEdge(root, pkg.Fset, EdgeSelection, positionID(pkg.Fset, sel.Pos()), qualifiedObject(selection.Obj()), sel.Pos()))
	}
	for importPath := range pkg.Imports {
		edges = append(edges, Edge{Kind: EdgeImport, From: pkg.PkgPath, To: importPath})
	}
	return edges
}

func modulePatterns(root string) []string {
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return []string{"./..."}
	}
	var patterns []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "node_modules":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Name() != "go.mod" {
			return nil
		}
		dir := filepath.Dir(path)
		rel, relErr := filepath.Rel(root, dir)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			return nil
		}
		if rel == "." {
			patterns = append(patterns, "./...")
		} else {
			patterns = append(patterns, "./"+filepath.ToSlash(rel)+"/...")
		}
		return nil
	})
	if len(patterns) == 0 {
		return []string{"./..."}
	}
	slices.Sort(patterns)
	return slices.Compact(patterns)
}

func qualifiedObject(obj types.Object) string {
	pkgPath := "builtin"
	if obj.Pkg() != nil {
		pkgPath = obj.Pkg().Path()
	}
	receiver := ""
	kind := fmt.Sprintf("%T", obj)
	if fn, ok := obj.(*types.Func); ok {
		if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
			receiver = types.TypeString(sig.Recv().Type(), func(p *types.Package) string { return p.Path() })
		}
	}
	return strings.Join([]string{pkgPath, receiver, obj.Name(), kind}, "|")
}

func positionID(fset *token.FileSet, pos token.Pos) string {
	p := fset.Position(pos)
	return fmt.Sprintf("%s:%d:%d", filepath.ToSlash(p.Filename), p.Line, p.Column)
}

func objectEdge(root string, fset *token.FileSet, kind EdgeKind, from, to string, pos token.Pos) Edge {
	p := fset.Position(pos)
	file := p.Filename
	if rel, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rel, "..") {
		file = filepath.ToSlash(rel)
	}
	return Edge{Kind: kind, From: from, To: to, File: file, Line: p.Line}
}

func findImplementations(root string, pkgs []*packages.Package) []Implementation {
	type candidate struct {
		pkg  *packages.Package
		name *types.TypeName
	}
	var named, interfaces []candidate
	for _, pkg := range pkgs {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			c := candidate{pkg: pkg, name: obj}
			if _, ok := obj.Type().Underlying().(*types.Interface); ok {
				interfaces = append(interfaces, c)
			} else if _, ok := obj.Type().(*types.Named); ok {
				named = append(named, c)
			}
		}
	}
	var cache typeutil.MethodSetCache
	var out []Implementation
	for _, concrete := range named {
		namedType := concrete.name.Type().(*types.Named)
		_ = cache.MethodSet(namedType)
		_ = cache.MethodSet(types.NewPointer(namedType))
		for _, iface := range interfaces {
			ifaceType := iface.name.Type().Underlying().(*types.Interface).Complete()
			if ifaceType.NumMethods() == 0 || (!types.Implements(namedType, ifaceType) && !types.Implements(types.NewPointer(namedType), ifaceType)) {
				continue
			}
			pos := concrete.pkg.Fset.Position(concrete.name.Pos())
			rel, err := filepath.Rel(root, pos.Filename)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			out = append(out, Implementation{TypeFile: filepath.ToSlash(rel), TypeName: concrete.name.Name(), InterfacePath: iface.pkg.PkgPath, InterfaceName: iface.name.Name()})
		}
	}
	return out
}

func buildCalls(root string, pkgs []*packages.Package) []CallEdge {
	prog, _ := ssautil.Packages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	graph := cha.CallGraph(prog)
	resolve := func(pos token.Pos) (string, int) {
		if !pos.IsValid() {
			return "", 0
		}
		p := prog.Fset.Position(pos)
		name := p.Filename
		if rel, err := filepath.Rel(root, name); err == nil && !strings.HasPrefix(rel, "..") {
			name = filepath.ToSlash(rel)
		}
		return name, p.Line
	}
	var out []CallEdge
	for fn, node := range graph.Nodes {
		if fn == nil {
			continue
		}
		for _, edge := range node.Out {
			if edge.Site == nil || !edge.Site.Pos().IsValid() || edge.Callee == nil || edge.Callee.Func == nil || edge.Callee.Func.Synthetic != "" {
				continue
			}
			callerFile, callerLine := resolve(edge.Site.Pos())
			calleeFile, _ := resolve(edge.Callee.Func.Pos())
			out = append(out, CallEdge{CallerFile: callerFile, CallerSymbol: fn.Name(), CallerLine: callerLine, CalleeFile: calleeFile, CalleeSymbol: edge.Callee.Func.Name(), CalleeReceiver: receiverName(edge.Callee.Func.Signature)})
		}
	}
	return out
}

func receiverName(signature *types.Signature) string {
	if signature == nil || signature.Recv() == nil {
		return ""
	}
	typeValue := signature.Recv().Type()
	prefix := ""
	if pointer, ok := typeValue.(*types.Pointer); ok {
		prefix = "*"
		typeValue = pointer.Elem()
	}
	if named, ok := typeValue.(*types.Named); ok && named.Obj() != nil {
		return prefix + named.Obj().Name()
	}
	return ""
}

func compareEdge(a, b Edge) int {
	return strings.Compare(fmt.Sprint(a.Kind, "\x00", a.File, "\x00", a.Line, "\x00", a.From, "\x00", a.To), fmt.Sprint(b.Kind, "\x00", b.File, "\x00", b.Line, "\x00", b.From, "\x00", b.To))
}
func compareCall(a, b CallEdge) int {
	return strings.Compare(fmt.Sprint(a.CalleeFile, "\x00", a.CalleeSymbol, "\x00", a.CallerFile, "\x00", a.CallerLine), fmt.Sprint(b.CalleeFile, "\x00", b.CalleeSymbol, "\x00", b.CallerFile, "\x00", b.CallerLine))
}
func compareImplementation(a, b Implementation) int {
	return strings.Compare(fmt.Sprint(a.TypeFile, "\x00", a.TypeName, "\x00", a.InterfacePath, "\x00", a.InterfaceName), fmt.Sprint(b.TypeFile, "\x00", b.TypeName, "\x00", b.InterfacePath, "\x00", b.InterfaceName))
}
