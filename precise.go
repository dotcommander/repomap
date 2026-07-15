package repomap

import (
	"path/filepath"

	"github.com/dotcommander/repomap/internal/callgraph"
)

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
		key := semanticCallsKey(e.CalleeFile, e.CalleeReceiver, e.CalleeSymbol)
		out[key] = append(out[key], Location{File: e.CallerFile, Line: e.CallerLine})
	}
	return out
}

// CallersFor returns the caller locations recorded for the given file+symbol,
// or nil if the symbol has no recorded callers. It builds the same lookup key
// used at construction (callsKey); on an exact-key miss it retries with
// forward-slash-normalized separators (filepath.ToSlash) so a lookup passing an
// OS-native path still matches keys built with root-relative slashed paths.
func (sc SymbolCallers) CallersFor(file, symbol string) []Location {
	if locs, ok := sc[callsKey(file, symbol)]; ok {
		return locs
	}
	if slash := filepath.ToSlash(file); slash != file {
		return sc[callsKey(slash, symbol)]
	}
	return nil
}

// CallersForSymbol resolves receiver-qualified semantic caller keys and falls
// back to the legacy file/name key for cached and external caller maps.
func (sc SymbolCallers) CallersForSymbol(file string, symbol Symbol) []Location {
	if locations, ok := sc[semanticCallsKey(file, symbol.Receiver, symbol.Name)]; ok {
		return locations
	}
	return sc.CallersFor(file, symbol.Name)
}

func semanticCallsKey(file, receiver, symbol string) string {
	if receiver == "" {
		return callsKey(file, symbol)
	}
	return callsKey(file, receiver+"."+symbol)
}
