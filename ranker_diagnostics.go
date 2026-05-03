package repomap

import "path/filepath"

// buildInternalPackageSet returns the set of all Go import paths present in the project.
func buildInternalPackageSet(files []*FileSymbols) map[string]bool {
	internalPkgs := make(map[string]bool)
	for _, f := range files {
		if f.ImportPath != "" {
			internalPkgs[f.ImportPath] = true
		}
	}
	return internalPkgs
}

// countInternalImports returns the number of distinct internal imports for a file.
// For Go, only imports matching a known internal package are counted.
// For non-Go, total distinct imports are used if above a noise threshold.
func countInternalImports(f RankedFile, internalPkgs map[string]bool) int {
	if len(f.Imports) == 0 {
		return 0
	}

	// Go: count only imports that match known internal packages.
	if f.Language == "go" && len(internalPkgs) > 0 {
		seen := make(map[string]bool, len(f.Imports))
		count := 0
		for _, imp := range f.Imports {
			if internalPkgs[imp] && !seen[imp] {
				seen[imp] = true
				count++
			}
		}
		return count
	}

	// Non-Go fallback: total distinct imports, but only if above noise threshold.
	// Below 3, most imports are stdlib/framework — the signal is noise.
	const nonGoThreshold = 3
	seen := make(map[string]bool, len(f.Imports))
	for _, imp := range f.Imports {
		seen[imp] = true
	}
	if len(seen) < nonGoThreshold {
		return 0
	}
	return len(seen)
}

// diagnosticPackageKey returns the grouping key for test coverage detection.
// Uses Go import path when available, otherwise the file's directory.
func diagnosticPackageKey(f *FileSymbols) string {
	if f.ImportPath != "" {
		return f.ImportPath
	}
	return filepath.Dir(f.Path)
}

// buildTestCoverageMap returns a set of package keys that have test coverage.
// A package is covered if any test file in it contains test symbols.
func buildTestCoverageMap(files []*FileSymbols) map[string]bool {
	covered := make(map[string]bool)
	for _, f := range files {
		if !isTestFile(f.Path) {
			continue
		}
		if f.Language == "go" {
			for _, s := range f.Symbols {
				if isTestSymbol(f.Path, s) {
					covered[diagnosticPackageKey(f)] = true
					break
				}
			}
		} else if len(f.Symbols) > 0 {
			covered[diagnosticPackageKey(f)] = true
		}
	}
	return covered
}

// shouldTagUntested reports whether a file should be flagged as lacking test coverage.
func shouldTagUntested(f RankedFile, covered map[string]bool) bool {
	if isTestFile(f.Path) {
		return false
	}

	// Only flag files with exported symbols.
	hasExported := false
	for _, s := range f.Symbols {
		if s.Exported {
			hasExported = true
			break
		}
	}
	if !hasExported {
		return false
	}

	return !covered[diagnosticPackageKey(f.FileSymbols)]
}
