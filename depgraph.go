package repomap

import (
	"fmt"
	"sort"
	"strings"
)

// formatDependencyGraph builds a compact package dependency graph header.
// Only shows Go packages with import paths and at least one internal dependency.
func formatDependencyGraph(files []RankedFile) string {
	internalPkgs := make(map[string]bool)
	for _, f := range files {
		if f.ImportPath != "" {
			internalPkgs[f.ImportPath] = true
		}
	}
	if len(internalPkgs) < 2 {
		return ""
	}

	// Map package import path to its internal dependencies (deduped).
	pkgDeps := make(map[string]map[string]bool)
	for _, f := range files {
		if f.ImportPath == "" {
			continue
		}
		for _, imp := range f.Imports {
			if internalPkgs[imp] && imp != f.ImportPath {
				if pkgDeps[f.ImportPath] == nil {
					pkgDeps[f.ImportPath] = make(map[string]bool)
				}
				pkgDeps[f.ImportPath][imp] = true
			}
		}
	}

	if len(pkgDeps) == 0 {
		return ""
	}

	// Find shortest common prefix to trim for readability.
	allPaths := make([]string, 0, len(internalPkgs))
	for p := range internalPkgs {
		allPaths = append(allPaths, p)
	}
	sort.Strings(allPaths)
	prefix := longestCommonPrefix(allPaths)
	if idx := strings.LastIndex(prefix, "/"); idx >= 0 {
		prefix = prefix[:idx+1]
	}

	var b strings.Builder
	fmt.Fprint(&b, "### Dependencies\n")

	// Sort packages for deterministic output.
	sortedPkgs := make([]string, 0, len(pkgDeps))
	for p := range pkgDeps {
		sortedPkgs = append(sortedPkgs, p)
	}
	sort.Strings(sortedPkgs)

	for _, pkg := range sortedPkgs {
		deps := pkgDeps[pkg]
		depNames := make([]string, 0, len(deps))
		for d := range deps {
			depNames = append(depNames, strings.TrimPrefix(d, prefix))
		}
		sort.Strings(depNames)
		fmt.Fprintf(&b, "%s → %s\n", strings.TrimPrefix(pkg, prefix), strings.Join(depNames, ", "))
	}

	return b.String()
}
