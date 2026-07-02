package repomap

import "github.com/dotcommander/repomap/internal/callgraph"

// TypedGraphToSymbolCallers converts the typed call graph's edges
// (precise-callgraph.md Decision 3) into the SymbolCallers shape every --calls
// consumer already reads, keyed by callsKey(calleeFile, calleeSymbol).
//
// The adapter does NOT filter against any RankedFile.Symbols set: an edge whose
// callee is absent from a rendered file's symbols is still present in the
// returned map and is simply never looked up at render time (calls_render.go's
// callers[callsKey(f.Path, s.Name)]) — the silent-drop point named in
// Decision 3's edge cases.
func TypedGraphToSymbolCallers(edges []callgraph.CallEdge) SymbolCallers {
	out := make(SymbolCallers)
	for _, e := range edges {
		key := callsKey(e.CalleeFile, e.CalleeSymbol)
		out[key] = append(out[key], Location{File: e.CallerFile, Line: e.CallerLine})
	}
	return out
}
