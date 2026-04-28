package repomap

import (
	"path/filepath"
	"strings"
)

// codeExts lists source-code extensions that, when newly added, promote a
// group's type to "feat" regardless of how many doc files are present.
var codeExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".rs": true, ".php": true, ".rb": true, ".java": true,
	".c": true, ".cpp": true, ".cs": true,
}

// docExts lists documentation/text extensions that keep type as "docs".
var docExts = map[string]bool{
	".md": true, ".txt": true, ".rst": true, ".adoc": true,
}

// groupInferType overrides the per-file dominant type with group-level rules:
//   - Any newly-added code file (Status "A" or "?") → "feat"
//   - All files are doc extensions → "docs"
//   - Otherwise the passed-in domType is returned unchanged.
func groupInferType(domType string, paths []string, byPath map[string]*fileChange) string {
	allDoc := true
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		f := byPath[p]
		isAdded := f != nil && (f.Status == "A" || f.IndexStatus == "A" || f.Status == "?")
		if isAdded && codeExts[ext] {
			return "feat"
		}
		if !docExts[ext] {
			allDoc = false
		}
	}
	if allDoc && len(paths) > 0 {
		return "docs"
	}
	return domType
}

// crossesPluginBoundary reports whether two paths live in different top-level
// plugins (e.g. plugins/dc/... vs plugins/pi/...). Files outside `plugins/`
// are never considered to cross.
func crossesPluginBoundary(a, b string) bool {
	aSeg := pluginSegment(a)
	bSeg := pluginSegment(b)
	return aSeg != "" && bSeg != "" && aSeg != bSeg
}

// pluginSegment returns the second path segment when the first is "plugins",
// else "".
func pluginSegment(path string) string {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) >= 2 && parts[0] == "plugins" {
		return parts[1]
	}
	return ""
}

// dominantVerb returns the action verb that best describes the group's git
// operation, derived from per-file Status (worktree) and IndexStatus (index):
//   - All files added  → "add"
//   - All files deleted → "remove"
//   - Otherwise         → "update"
func dominantVerb(paths []string, byPath map[string]*fileChange) string {
	allAdd, allDel := len(paths) > 0, len(paths) > 0
	for _, p := range paths {
		f := byPath[p]
		if f == nil {
			allAdd, allDel = false, false
			break
		}
		isAdd := f.Status == "A" || f.IndexStatus == "A" || f.Status == "?"
		isDel := f.Status == "D" || f.IndexStatus == "D"
		if !isAdd {
			allAdd = false
		}
		if !isDel {
			allDel = false
		}
	}
	switch {
	case allAdd:
		return "add"
	case allDel:
		return "remove"
	default:
		return "update"
	}
}
