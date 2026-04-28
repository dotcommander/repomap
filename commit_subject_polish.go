package repomap

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Polish generates a heuristic commit subject for a CommitGroup.
// Returns (subject, confidence) where confidence >= 0.6 is safe to use
// without LLM review. Below 0.6, callers should mark the group for LLM polish.
//
// Algorithm: classify files by extension family and directory prefix, then
// combine with a diff-stat action verb to produce a templated subject.
// Specificity rules:
//   - test files  → type=test, confidence 0.7
//   - single file, known family → confidence depends on scope clarity
//   - mixed / unknown → chore fallback at 0.3
func Polish(g CommitGroup) (subject string, confidence float64) {
	if len(g.Files) == 0 {
		return "chore: update files", 0.3
	}

	family := classifyFileFamily(g.Files)
	dir := topCommitDir(g.Files)
	action := classifyAction(g.Type, g.Verb)

	// Resolve conventional type from family; use group type when available.
	commitType := resolveCommitType(g.Type, family)

	// Build scope from longest-common-prefix path capped at 1 segment.
	scope := dir
	if scope == "." {
		scope = ""
	}

	// Compose subject.
	what := familyWhat(family, len(g.Files))

	var subject2 string
	if scope != "" {
		subject2 = fmt.Sprintf("%s(%s): %s %s", commitType, scope, action, what)
	} else {
		subject2 = fmt.Sprintf("%s: %s %s", commitType, action, what)
	}

	// Confidence calculation.
	conf := confidenceFor(family, scope, g.Files)

	return subject2, conf
}

// configExts and scriptExts complement codeExts/docExts (commit_plugin_boundary.go)
// to cover the full extension space used by fileFamily.
var (
	configExts = map[string]bool{
		".yaml": true, ".yml": true, ".json": true, ".toml": true,
		".ini": true, ".cfg": true, ".conf": true, ".env": true, ".hcl": true,
	}
	scriptExts = map[string]bool{
		".sh": true, ".bash": true, ".zsh": true, ".fish": true,
	}
)

// fileFamily classifies a single file path into a named family.
// Returns one of: "test", "code", "docs", "config", "script", "other".
func fileFamily(path string) string {
	// Test files take priority over other classifications.
	// Uses the package-level isTestFile(path) from categorize.go.
	if isTestFile(path) {
		return "test"
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case codeExts[ext]:
		return "code"
	case docExts[ext]:
		return "docs"
	case configExts[ext]:
		return "config"
	case scriptExts[ext]:
		return "script"
	}
	return "other"
}

// classifyFileFamily returns the dominant family across all files.
// If all files are the same family, returns that family.
// If mixed, returns "other".
func classifyFileFamily(files []string) string {
	if len(files) == 0 {
		return "other"
	}
	counts := make(map[string]int, 6)
	for _, f := range files {
		counts[fileFamily(f)]++
	}
	// Pure single-family?
	if len(counts) == 1 {
		for k := range counts {
			return k
		}
	}
	// Dominant: if one family is >50% of files.
	for k, c := range counts {
		if float64(c)/float64(len(files)) > 0.5 {
			return k
		}
	}
	return "other"
}

// topCommitDir returns the dominant top-level directory for a set of files.
// Uses the longest common path prefix capped at 1 segment; falls back to ".".
func topCommitDir(files []string) string {
	if len(files) == 0 {
		return "."
	}
	// Gather first-segment dirs.
	dirs := make(map[string]int, len(files))
	for _, f := range files {
		dirs[topDir(f)]++
	}
	if len(dirs) == 1 {
		for k := range dirs {
			return k
		}
	}
	// Pick the most frequent non-root dir.
	best, bestCount := ".", 0
	for d, c := range dirs {
		if c > bestCount || (c == bestCount && d < best) {
			best, bestCount = d, c
		}
	}
	return best
}

// classifyAction returns an action verb from the group type and dominant verb.
// groupType "fix" always wins; "feat" always uses "add" (Conventional Commits:
// feat = new capability = something was added, regardless of git Status).
// Otherwise the dominant verb from git status is used so newly-added files get
// "add" and deleted files get "remove".
func classifyAction(groupType, verb string) string {
	switch groupType {
	case "fix":
		return "fix"
	case "feat":
		// feat always implies addition by definition.
		return "add"
	}
	if verb != "" {
		return verb
	}
	return "update"
}

// resolveCommitType picks the conventional commit type.
// Group type overrides the family heuristic when it's a concrete type.
func resolveCommitType(groupType, family string) string {
	switch groupType {
	case "feat", "fix", "refactor", "perf", "style", "build", "ci", "revert":
		return groupType
	case "docs":
		return "docs"
	case "test":
		return "test"
	case "chore", "deps":
		return "chore"
	}
	// Fall through to family-based heuristic.
	switch family {
	case "test":
		return "test"
	case "docs":
		return "docs"
	case "config", "script":
		return "chore"
	case "code":
		return "chore" // conservative: can't distinguish feat/fix without diff
	}
	return "chore"
}

// familyWhat returns a human-readable "what" phrase for the subject.
func familyWhat(family string, n int) string {
	if n == 1 {
		switch family {
		case "test":
			return "tests"
		case "docs":
			return "notes"
		case "config":
			return "config"
		case "script":
			return "script"
		case "code":
			return "implementation"
		}
		return "files"
	}
	// Plural.
	count := fmt.Sprintf("%d", n)
	switch family {
	case "test":
		return count + " test files"
	case "docs":
		return "notes"
	case "config":
		return count + " config files"
	case "script":
		return count + " scripts"
	case "code":
		return count + " files"
	}
	return count + " files"
}

// confidenceFor computes the confidence score for a generated subject.
//
// Scoring:
//   - Pure test family: 0.7
//   - Pure docs family: 0.7
//   - Pure config/script: 0.6
//   - Pure code family: 0.5 (type ambiguous — could be feat/fix/refactor)
//   - Mixed families (other): 0.4
//   - No scope (root-level mixed bag): -0.1
//   - Never emit WIP/misc; floor at 0.3
func confidenceFor(family, scope string, files []string) float64 {
	base := 0.4
	switch family {
	case "test":
		base = 0.7
	case "docs":
		base = 0.7
	case "config", "script":
		base = 0.6
	case "code":
		base = 0.5
	case "other":
		base = 0.4
	}
	// Scope adds specificity.
	if scope != "" && scope != "." {
		base += 0.05
	}
	// Single-file clarity.
	if len(files) == 1 {
		base += 0.05
	}
	// Floor.
	if base < 0.3 {
		base = 0.3
	}
	// Cap.
	if base > 0.95 {
		base = 0.95
	}
	return base
}

// PolishGroup is a convenience wrapper that applies Polish and updates
// g.SuggestedMsg when the result meets the confidence threshold. Returns
// whether the message was updated.
func PolishGroup(g *CommitGroup, threshold float64) bool {
	subject, conf := Polish(*g)
	if conf < threshold {
		return false
	}
	g.SuggestedMsg = subject
	g.Confidence = conf
	return true
}

// buildFallbackSubject builds a rock-bottom fallback for when confidence < 0.3.
// Always returns a valid conventional commit subject.
func buildFallbackSubject(files []string) string {
	n := len(files)
	if n == 0 {
		return "chore: update files"
	}
	return fmt.Sprintf("chore: update %d files", n)
}
