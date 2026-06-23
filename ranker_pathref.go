package repomap

import (
	"path/filepath"
	"strings"
)

// pathKey normalizes a file path to its extension-stripped, slash-normalized
// form used as the cross-file matching key for non-Go languages.
// e.g. "src/foo/types.ts" -> "src/foo/types".
func pathKey(path string) string {
	p := filepath.ToSlash(path)
	return strings.TrimSuffix(p, filepath.Ext(p))
}

// stripImportQuotes removes a single pair of surrounding ' or " or ` from a
// stored import string. Tree-sitter stores import sources WITH their quote
// delimiters (e.g. `"./types"`), so they must be stripped before resolution.
func stripImportQuotes(imp string) string {
	imp = strings.TrimSpace(imp)
	if len(imp) >= 2 {
		switch imp[0] {
		case '\'', '"', '`':
			if imp[len(imp)-1] == imp[0] {
				return imp[1 : len(imp)-1]
			}
		}
	}
	return imp
}

// isRelativeImport reports whether a (quote-stripped) import is a relative
// path import ("./x" or "../x"), as opposed to a bare package/module import.
func isRelativeImport(imp string) bool {
	return strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../")
}

// resolveRelativeImportKey resolves a relative import to a normalized
// path-without-ext key, relative to the importing file's directory.
// It strips quotes, joins against the importer dir, cleans the path, and
// strips a trailing extension. Directory imports ("./foo" where the target is
// a dir) resolve to "<dir>/foo"; the candidateKeys helper lets callers also
// try the "<dir>/foo/index" form. Returns "" if imp is not relative.
func resolveRelativeImportKey(importerPath, imp string) string {
	imp = stripImportQuotes(imp)
	if !isRelativeImport(imp) {
		return ""
	}
	dir := filepath.Dir(filepath.ToSlash(importerPath))
	joined := filepath.ToSlash(filepath.Join(dir, imp))
	return strings.TrimSuffix(joined, filepath.Ext(joined))
}

// nonGoImportKeys returns the candidate match keys for a single import string
// originating from importerPath, given the set of existing file keys.
// - Relative imports resolve path-aware: the resolved key, plus an "/index"
//   variant to cover directory imports ("./components" -> "./components/index").
// - Bare/package imports fall back to bare-basename matching (Python dotted
//   modules, npm packages, C system headers) — path resolution is meaningless
//   for them.
// Only keys present in existingKeys are returned, so callers can match without
// guessing which candidate is real.
func nonGoImportKeys(importerPath, imp string, existingKeys map[string]struct{}) []string {
	stripped := stripImportQuotes(imp)
	if isRelativeImport(stripped) {
		base := resolveRelativeImportKey(importerPath, imp)
		out := make([]string, 0, 2)
		if _, ok := existingKeys[base]; ok {
			out = append(out, base)
		}
		idx := base + "/index"
		if _, ok := existingKeys[idx]; ok {
			out = append(out, idx)
		}
		return out
	}
	// Bare/package import: basename fallback.
	bn := basenameWithoutExt(stripped)
	if bn == "" {
		return nil
	}
	if _, ok := existingKeys[bn]; ok {
		return []string{bn}
	}
	return nil
}
