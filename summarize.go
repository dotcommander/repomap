package repomap

import (
	"fmt"
	"sort"
	"strings"
)

const groupPreviewLimit = 4

type symbolGroup struct {
	label   string
	summary string
	count   int
}

func summarizeGroup(category string, syms []Symbol) string {
	names := make([]string, 0, len(syms))

	if category == "methods" {
		// Check for duplicate method names that need receiver qualification.
		nameCounts := make(map[string]int)
		for _, s := range syms {
			nameCounts[s.Name]++
		}
		for _, s := range syms {
			if nameCounts[s.Name] > 1 && s.Receiver != "" {
				names = append(names, s.Receiver+"."+s.Name)
			} else {
				names = append(names, s.Name)
			}
		}
	} else {
		for _, s := range syms {
			names = append(names, s.Name)
		}
	}

	sort.Strings(names)
	if collapsed, ok := collapseCommonPrefix(names); ok {
		return withTotal(collapsed, len(names), true)
	}
	return withTotal(previewNames(names), len(names), len(names) > groupPreviewLimit)
}

func previewNames(names []string) string {
	if len(names) <= groupPreviewLimit {
		return strings.Join(names, ", ")
	}
	preview := append([]string{}, names[:groupPreviewLimit]...)
	preview = append(preview, "...")
	return strings.Join(preview, ", ")
}

func withTotal(summary string, total int, forced bool) string {
	if total == 0 {
		return ""
	}
	if forced || total > 1 {
		return fmt.Sprintf("%s (%d total)", summary, total)
	}
	return summary
}

func collapseCommonPrefix(names []string) (string, bool) {
	if len(names) < 3 {
		return "", false
	}

	prefix := longestCommonPrefix(names)
	if len(prefix) < 3 {
		return "", false
	}
	prefix, _ = strings.CutSuffix(prefix, "_")
	if len(prefix) < 3 {
		return "", false
	}

	suffixes := make([]string, 0, len(names))
	for _, name := range names {
		suffix := strings.TrimPrefix(name, prefix)
		suffix = strings.TrimPrefix(suffix, "_")
		if suffix == "" {
			return "", false
		}
		suffixes = append(suffixes, suffix)
	}

	preview := suffixes
	truncated := false
	if len(preview) > groupPreviewLimit {
		preview = append([]string{}, preview[:groupPreviewLimit]...)
		truncated = true
	}

	body := strings.Join(preview, ", ")
	if truncated {
		body += ", ..."
	}
	return fmt.Sprintf("%s{%s}", prefix, body), true
}

func longestCommonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	prefix := names[0]
	for _, name := range names[1:] {
		for !strings.HasPrefix(name, prefix) {
			if prefix == "" {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return trimIdentifierPrefix(prefix)
}

func trimIdentifierPrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	lastBoundary := -1
	for i := 1; i < len(prefix); i++ {
		if prefix[i] == '_' || isCamelBoundary(prefix[i-1], prefix[i]) {
			lastBoundary = i
		}
	}
	if lastBoundary > 0 {
		return prefix[:lastBoundary]
	}
	return prefix
}

func isCamelBoundary(prev, curr byte) bool {
	return prev >= 'a' && prev <= 'z' && curr >= 'A' && curr <= 'Z'
}
