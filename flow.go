package repomap

import (
	"fmt"
	"path/filepath"
	"strings"
)

// fileDepth returns a file's directory depth, mirroring applyDepthPenalty:
// the number of path separators in the relative path.
func fileDepth(path string) int {
	return strings.Count(path, string(filepath.Separator))
}

// hasBehavioralSymbol reports whether a file declares at least one exported
// function or method — the signal that a file carries behavior, not just DTOs.
func hasBehavioralSymbol(f RankedFile) bool {
	for _, s := range f.Symbols {
		if s.Exported && (s.Kind == "function" || s.Kind == "method") {
			return true
		}
	}
	return false
}

// formatFlowSpine renders a compact orientation block: the entry file plus the
// top few behavioral "spine" files in ranked order. It performs no graph
// computation — spine = the first qualifying files in the already-Score-sorted
// slice. Returns "" when there is no entry file (additive no-op).
func formatFlowSpine(files []RankedFile) string {
	const spineLimit = 5

	// Find the entry file: Tag=="entry" with lowest depth (prefer main.go).
	var entry *RankedFile
	for i := range files {
		if files[i].Tag != "entry" {
			continue
		}
		f := &files[i]
		if entry == nil {
			entry = f
			continue
		}
		base := filepath.Base(f.Path)
		entryBase := filepath.Base(entry.Path)
		if base == "main.go" && entryBase != "main.go" {
			entry = f
			continue
		}
		if (base == "main.go") == (entryBase == "main.go") && fileDepth(f.Path) < fileDepth(entry.Path) {
			entry = f
		}
	}
	if entry == nil {
		return ""
	}

	// spine = first 5 ranked files that are non-test, non-entry, depth ≤ 1,
	// and carry an exported function or method.
	var spine []string
	for i := range files {
		f := files[i]
		if f.Tag == "entry" || isTestFile(f.Path) || fileDepth(f.Path) > 1 {
			continue
		}
		if !hasBehavioralSymbol(f) {
			continue
		}
		spine = append(spine, f.Path)
		if len(spine) == spineLimit {
			break
		}
	}

	var b strings.Builder
	fmt.Fprint(&b, "### Flow\n")
	fmt.Fprintf(&b, "entry: %s\n", entry.Path)
	if len(spine) > 0 {
		fmt.Fprintf(&b, "spine: %s\n", strings.Join(spine, ", "))
	}
	fmt.Fprint(&b, "\n")
	return b.String()
}
