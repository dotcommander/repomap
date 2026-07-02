package callgraph

import (
	"context"
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// CallEdge is one resolved caller→callee relationship in the typed call graph.
// It carries enough structure to build SymbolCallers via the root package's
// TypedGraphToSymbolCallers adapter (Decision 3).
type CallEdge struct {
	CallerFile, CallerSymbol string
	CallerLine               int
	CalleeFile, CalleeSymbol string
}

// ErrLoadFailed is the exported sentinel returned when go/packages fails to
// load any usable package. Callers catch it via errors.Is and fall back to the
// gopls --calls tier (never a hard CLI error).
var ErrLoadFailed = errors.New("callgraph: go/packages load failed")

// Build returns the type-checked whole-program call graph for the module rooted
// at root, computed with go/packages (LoadSyntax) → go/ssa → Class Hierarchy
// Analysis (precise-callgraph.md Decisions 1 and 2).
//
// Loading is fail-open: a package with type errors is skipped with a single
// stderr warning and the remaining packages are still analyzed. Only a total
// failure — packages.Load returning an error, or zero usable packages — returns
// ErrLoadFailed, which the caller catches to fall back to the gopls --calls tier.
func Build(ctx context.Context, root string) ([]CallEdge, error) {
	cfg := &packages.Config{Mode: packages.LoadSyntax, Dir: root, Context: ctx}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLoadFailed, err)
	}

	var usable []*packages.Package
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			fmt.Fprintf(os.Stderr, "repomap: --precise: skipping %s (%d errors)\n", p.PkgPath, len(p.Errors))
			continue
		}
		usable = append(usable, p)
	}
	if len(usable) == 0 {
		return nil, ErrLoadFailed
	}

	prog, _ := ssautil.Packages(usable, ssa.InstantiateGenerics)
	prog.Build()
	graph := cha.CallGraph(prog)

	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	// resolve maps a token.Pos to a root-relative filename and 1-based line,
	// matching RankedFile.Path's root-relative convention.
	resolve := func(p token.Pos) (string, int) {
		if !p.IsValid() {
			return "", 0
		}
		pos := prog.Fset.Position(p)
		name := pos.Filename
		if rel, relErr := filepath.Rel(absRoot, name); relErr == nil {
			name = rel
		}
		return name, pos.Line
	}

	var edges []CallEdge
	for fn, node := range graph.Nodes {
		if fn == nil {
			continue // synthetic root node
		}
		for _, e := range node.Out {
			// Report only calls made from a real source call site: synthetic
			// wrapper/thunk callers carry no call-site position.
			if e.Site == nil || !e.Site.Pos().IsValid() {
				continue
			}
			// Attribute each call to the real source callee: skip
			// compiler-synthesized callees (pointer-receiver wrappers, thunks,
			// bound methods). CHA resolves an interface invoke to every
			// implementer's real method AND its synthesized pointer wrapper,
			// which collapse to the same (file, symbol); keeping the wrappers
			// double-counts. The real method is always present as its own edge
			// (precise-callgraph.md Decision 1/3: synthetic edges are dropped).
			if e.Callee == nil || e.Callee.Func == nil || e.Callee.Func.Synthetic != "" {
				continue
			}
			callerFile, callerLine := resolve(e.Site.Pos())
			calleeFile, _ := resolve(e.Callee.Func.Pos())
			edges = append(edges, CallEdge{
				CallerFile:   callerFile,
				CallerSymbol: fn.Name(),
				CallerLine:   callerLine,
				CalleeFile:   calleeFile,
				CalleeSymbol: e.Callee.Func.Name(),
			})
		}
	}
	return edges, nil
}
