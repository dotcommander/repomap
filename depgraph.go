package repomap

import (
	"fmt"
	"slices"
	"strings"
)

// buildPackageDeps returns a deterministic map of internal-package imports
// (pkg → sorted list of internal packages it depends on), plus the set of
// internal packages. Returns nil, nil when there are fewer than 2 internal
// packages or no internal edges.
func buildPackageDeps(files []RankedFile) (deps map[string][]string, internal map[string]bool) {
	internal = make(map[string]bool)
	for _, f := range files {
		if f.ImportPath != "" {
			internal[f.ImportPath] = true
		}
	}
	if len(internal) < 2 {
		return nil, nil
	}

	// Dedupe per-package dependencies via set, then flatten to sorted slices.
	pkgDeps := make(map[string]map[string]bool)
	for _, f := range files {
		if f.ImportPath == "" {
			continue
		}
		for _, imp := range f.Imports {
			if internal[imp] && imp != f.ImportPath {
				if pkgDeps[f.ImportPath] == nil {
					pkgDeps[f.ImportPath] = make(map[string]bool)
				}
				pkgDeps[f.ImportPath][imp] = true
			}
		}
	}
	if len(pkgDeps) == 0 {
		return nil, nil
	}

	deps = make(map[string][]string, len(pkgDeps))
	for pkg, set := range pkgDeps {
		names := make([]string, 0, len(set))
		for d := range set {
			names = append(names, d)
		}
		slices.Sort(names)
		deps[pkg] = names
	}
	return deps, internal
}

// sortedPkgs returns the keys of a deps map in ascending order.
func sortedPkgs(deps map[string][]string) []string {
	keys := make([]string, 0, len(deps))
	for p := range deps {
		keys = append(keys, p)
	}
	slices.Sort(keys)
	return keys
}

// formatDependencyGraph builds a compact package dependency graph header.
// Only shows Go packages with import paths and at least one internal dependency.
func formatDependencyGraph(files []RankedFile) string {
	deps, internal := buildPackageDeps(files)
	if deps == nil {
		return ""
	}

	// Find shortest common prefix to trim for readability.
	allPaths := make([]string, 0, len(internal))
	for p := range internal {
		allPaths = append(allPaths, p)
	}
	slices.Sort(allPaths)
	prefix := longestCommonPrefix(allPaths)
	if idx := strings.LastIndex(prefix, "/"); idx >= 0 {
		prefix = prefix[:idx+1]
	}

	var b strings.Builder
	fmt.Fprint(&b, "### Dependencies\n")
	for _, pkg := range sortedPkgs(deps) {
		trimmed := make([]string, len(deps[pkg]))
		for i, d := range deps[pkg] {
			trimmed[i] = strings.TrimPrefix(d, prefix)
		}
		fmt.Fprintf(&b, "%s → %s\n", strings.TrimPrefix(pkg, prefix), strings.Join(trimmed, ", "))
	}
	return b.String()
}
