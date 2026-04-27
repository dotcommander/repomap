package repomap

import (
	"fmt"
	"regexp"
	"sort"
)

// conventionalSubjectRe is the strict validator for commit subject lines used
// by commit execute. isConventional (in commit_analyze.go) is intentionally
// permissive — it classifies existing history without failing on it. This regex
// is the gate that rejects bad messages before they land.
//
// Invariant: type list here must be a superset of the permissive list in
// isConventional, so that any message that passes this gate would also be
// counted as conventional by classifyHistoryStyle.
var conventionalSubjectRe = regexp.MustCompile(
	`^(feat|fix|refactor|docs|test|chore|perf|style|build|ci|revert)(\(.+\))?: .{1,72}$`,
)

// tagRe is the allowed semver tag format.
var tagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[A-Za-z0-9.-]+)?$`)

// ValidateConventionalMsg returns an error if the first line of msg does not
// match conventionalSubjectRe. Only the first line is validated; a multi-line
// body is allowed.
func ValidateConventionalMsg(msg string) error {
	first := msg
	if i := indexOf(msg, '\n'); i >= 0 {
		first = msg[:i]
	}
	if !conventionalSubjectRe.MatchString(first) {
		return fmt.Errorf("message %q does not match conventional format (type(scope)?: subject ≤72 chars)", first)
	}
	return nil
}

// ValidateTag returns an error if tag does not match the semver format.
func ValidateTag(tag string) error {
	if !tagRe.MatchString(tag) {
		return fmt.Errorf("tag %q does not match required format vX.Y.Z[-pre]", tag)
	}
	return nil
}

// consolidateGroups enforces the cap-3/fold-riders/merge-smallest rules from
// commit-agent.md §93-106. It is a pure function: input groups are not mutated.
//
// Algorithm (deterministic — sort before every decision):
//  1. Sort groups by file count desc, then ID alpha (stability).
//  2. If ≤3 groups, return as-is.
//  3. "Rider" = group with 1-2 files. Fold each rider into the largest group
//     that shares its top-level directory. Riders with no match are left alone.
//  4. If still >3 groups, merge the two smallest (by file count, then ID) into
//     the smaller one's entry, keeping the larger group's SuggestedMsg.
//  5. Repeat step 4 until ≤3.
func consolidateGroups(groups []CommitGroup) []CommitGroup {
	if len(groups) <= 3 {
		return groups
	}

	// Work on a shallow copy so callers see no mutation.
	gs := make([]CommitGroup, len(groups))
	copy(gs, groups)
	sortGroups(gs)

	// Step 3: fold riders into the largest same-directory group.
	gs = foldRiders(gs)
	sortGroups(gs)

	// Step 4-5: merge smallest pairs until ≤3.
	for len(gs) > 3 {
		gs = mergeTwoSmallest(gs)
		sortGroups(gs)
	}
	return gs
}

// topDir returns the top-level directory component of a file path,
// or "." for root-level files.
func topDir(path string) string {
	if i := indexOf(path, '/'); i > 0 {
		return path[:i]
	}
	return "."
}

// foldRiders folds rider groups (1-2 files) into the largest group sharing
// their top-level directory. Groups are already sorted desc by file count.
func foldRiders(gs []CommitGroup) []CommitGroup {
	out := make([]CommitGroup, 0, len(gs))
	for _, g := range gs {
		if len(g.Files) > 2 {
			out = append(out, g)
		}
	}

	// Riders: groups with 1-2 files.
	for _, rider := range gs {
		if len(rider.Files) > 2 {
			continue
		}
		// Find target: largest group (already first in out) sharing top-level dir.
		targetIdx := -1
		for i := range out {
			for _, f := range rider.Files {
				if topDir(f) == topDir(out[i].Files[0]) {
					targetIdx = i
					break
				}
			}
			if targetIdx >= 0 {
				break
			}
		}
		if targetIdx >= 0 {
			out[targetIdx].Files = append(out[targetIdx].Files, rider.Files...)
		} else {
			out = append(out, rider)
		}
	}
	return out
}

// mergeTwoSmallest merges the two smallest groups (by file count desc: last
// two) into one, keeping the larger group's SuggestedMsg.
func mergeTwoSmallest(gs []CommitGroup) []CommitGroup {
	// gs is sorted desc; the two smallest are the last two entries.
	n := len(gs)
	larger := gs[n-2]
	smaller := gs[n-1]
	larger.Files = append(larger.Files, smaller.Files...)
	result := make([]CommitGroup, n-1)
	copy(result, gs[:n-1])
	result[n-2] = larger
	return result
}

// sortGroups sorts groups by file count descending, then by ID ascending for
// determinism.
func sortGroups(gs []CommitGroup) {
	sort.SliceStable(gs, func(i, j int) bool {
		if len(gs[i].Files) != len(gs[j].Files) {
			return len(gs[i].Files) > len(gs[j].Files)
		}
		return gs[i].ID < gs[j].ID
	})
}

// indexOf returns the index of b in s, or -1 if not found.
// Named to avoid collision with strings.Index across callers.
func indexOf(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
