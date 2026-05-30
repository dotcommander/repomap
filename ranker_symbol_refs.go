package repomap

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	symbolRefsMaxFileBytes    = 1 << 20
	symbolRefsBonusPerRef     = 2
	symbolRefsMaxBonus        = 24
	symbolRefsMinNameLen      = 4
	symbolRefsMaxDocFreqRatio = 0.40 // skip names appearing in >40% of files (identifier-space stop-words)
)

// ApplySymbolReferenceBonus adds a cheap, approximate cross-language usage
// signal by counting how many other files mention each exported non-Go symbol.
//
// It is lexical by design: useful when import graphs are weak and LSP callers
// are unavailable, but lower-fidelity than --calls and therefore capped.
func ApplySymbolReferenceBonus(root string, ranked []RankedFile) {
	if root == "" || len(ranked) == 0 {
		return
	}

	type target struct {
		file string
		name string
	}

	byName := make(map[string][]target)
	for i := range ranked {
		rf := ranked[i]
		if rf.Language == "go" {
			continue
		}
		for _, sym := range rf.Symbols {
			if !sym.Exported || !symbolRefNameOK(sym.Name) {
				continue
			}
			byName[sym.Name] = append(byName[sym.Name], target{file: rf.Path, name: sym.Name})
		}
	}
	if len(byName) == 0 {
		return
	}

	// Identifier-space stop-word filter: drop names common enough to be coincidental.
	// A name exported by >40% of files is too generic to signal intentional coupling.
	// Floor: only apply when threshold would be ≥2 (avoids false drops in tiny repos).
	if docFreqThreshold := int(float64(len(ranked)) * symbolRefsMaxDocFreqRatio); docFreqThreshold >= 2 {
		for name, targets := range byName {
			if len(targets) > docFreqThreshold {
				delete(byName, name)
			}
		}
	}

	refFiles := make(map[string]map[string]struct{})
	for _, rf := range ranked {
		words, ok := fileIdentifierSet(filepath.Join(root, rf.Path))
		if !ok {
			continue
		}
		for word := range words {
			for _, tgt := range byName[word] {
				if tgt.file == rf.Path {
					continue
				}
				key := tgt.file + "\x00" + tgt.name
				if refFiles[key] == nil {
					refFiles[key] = make(map[string]struct{})
				}
				refFiles[key][rf.Path] = struct{}{}
			}
		}
	}

	fileRefs := make(map[string]int)
	for key, callers := range refFiles {
		idx := strings.IndexByte(key, '\x00')
		if idx < 0 {
			continue
		}
		fileRefs[key[:idx]] += len(callers)
	}

	for i := range ranked {
		count := fileRefs[ranked[i].Path]
		if count == 0 {
			continue
		}
		bonus := count * symbolRefsBonusPerRef
		if bonus > symbolRefsMaxBonus {
			bonus = symbolRefsMaxBonus
		}
		addScoreComponent(&ranked[i], scoreComponentSymbolRefs, bonus)
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Path < ranked[j].Path
	})
}

func symbolRefNameOK(name string) bool {
	if len(name) < symbolRefsMinNameLen {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func fileIdentifierSet(path string) (map[string]struct{}, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, symbolRefsMaxFileBytes+1))
	if err != nil || len(data) > symbolRefsMaxFileBytes {
		return nil, false
	}

	words := make(map[string]struct{})
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		words[b.String()] = struct{}{}
		b.Reset()
	}
	for _, r := range string(data) {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return words, true
}
