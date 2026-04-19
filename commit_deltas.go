package repomap

import (
	"context"
	"os"
	"path/filepath"
	"slices"
)

// computeSymbolDeltas returns a per-file diff of symbols between HEAD and the
// working tree. Uses the real language parsers (via parseDirtyFiles dispatch)
// for both pre and post, enabling signature-aware Modified detection.
// Missing-at-HEAD is treated as an all-added file.
func computeSymbolDeltas(ctx context.Context, root string, files []fileChange, postSymbols map[string]*FileSymbols) map[string]symbolDelta {
	out := make(map[string]symbolDelta, len(files))
	for _, f := range files {
		if f.Language == "" {
			continue
		}
		post := postSymbols[f.Path]
		postSigs := symbolSigMap(post)

		// Pure addition — no HEAD content; every post-symbol is "added".
		if f.IndexStatus == "A" && f.Status == "A" {
			var added []string
			for name := range postSigs {
				added = append(added, name)
			}
			slices.Sort(added)
			if len(added) > 0 {
				out[f.Path] = symbolDelta{Path: f.Path, Added: added}
			}
			continue
		}

		// Read HEAD content and parse with the same dispatcher used for post.
		var preSigs map[string]string // name → signature
		var preExported map[string]bool
		preSrc, _ := gitShowAt(ctx, root, "HEAD", oldPathOr(f))
		if preSrc != "" {
			preFS := parseFileSymbolsFromSource(root, f.Path, f.Language, preSrc)
			preSigs = symbolSigMap(preFS)
			preExported = symbolExportedMap(preFS)
		}

		postExported := symbolExportedMap(post)

		var added, removed, modified []string
		for name, postSig := range postSigs {
			if preSig, exists := preSigs[name]; !exists {
				added = append(added, name)
			} else if preSig != postSig {
				modified = append(modified, name)
			}
		}
		for name := range preSigs {
			if _, exists := postSigs[name]; !exists {
				removed = append(removed, name)
			}
		}
		slices.Sort(added)
		slices.Sort(removed)
		slices.Sort(modified)
		if len(added) == 0 && len(removed) == 0 && len(modified) == 0 {
			continue
		}

		// Breaking if any removed symbol was exported, or any modified symbol
		// was exported in both pre and post (public API signature change).
		breaking := false
		for _, name := range removed {
			if preExported[name] {
				breaking = true
				break
			}
		}
		if !breaking {
			for _, name := range modified {
				if preExported[name] && postExported[name] {
					breaking = true
					break
				}
			}
		}

		out[f.Path] = symbolDelta{
			Path:     f.Path,
			Added:    added,
			Removed:  removed,
			Modified: modified,
			Breaking: breaking,
		}
	}
	return out
}

// oldPathOr returns OldPath for renames, else Path. Needed so `git show HEAD:`
// resolves to the file's pre-rename name.
func oldPathOr(f fileChange) string {
	if f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}

// symbolSigMap returns a map of symbol name → signature for all symbols in fs.
// Returns an empty map when fs is nil (new/unparsable file). When two symbols
// share a name (overloads), the last one wins — commit-message diffing only
// needs to detect that a name was added, removed, or changed.
func symbolSigMap(fs *FileSymbols) map[string]string {
	out := make(map[string]string)
	if fs == nil {
		return out
	}
	for _, s := range fs.Symbols {
		out[s.Name] = s.Signature
	}
	return out
}

// symbolExportedMap returns a map of symbol name → whether it is publicly
// exported (Symbol.Exported=true). PHP public visibility is already folded
// into Exported by phpVisibilityToExported. Used for breaking-change detection.
func symbolExportedMap(fs *FileSymbols) map[string]bool {
	out := make(map[string]bool)
	if fs == nil {
		return out
	}
	for _, s := range fs.Symbols {
		if s.Exported {
			out[s.Name] = true
		}
	}
	return out
}

// parseFileSymbolsFromSource parses an in-memory source string (typically HEAD
// content from `git show`) through the same ladder as working-tree files:
// Go AST for Go, then parseNonGoFile (tree-sitter → regex) for everything
// else. Writes src to a temp file because the on-disk parsers require a path.
// Returns nil on parse failure.
func parseFileSymbolsFromSource(root, path, language, src string) *FileSymbols {
	tmp, err := os.CreateTemp("", "repomap-presym-*"+filepath.Ext(path))
	if err != nil {
		return nil
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(src); err != nil {
		tmp.Close()
		return nil
	}
	tmp.Close()

	if language == "go" {
		fs, _ := ParseGoFile(tmp.Name(), root)
		return fs
	}
	return parseNonGoFile(tmp.Name(), root, language)
}
