package repomap

import (
	"fmt"
	"slices"
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

	slices.Sort(names)
	if collapsed, truncated, ok := collapseCommonPrefix(names); ok {
		return withTotal(collapsed, len(names), truncated)
	}
	return withTotal(previewNames(names), len(names), len(names) > groupPreviewLimit)
}

func previewNames(names []string) string {
	if len(names) <= groupPreviewLimit {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:groupPreviewLimit], ", ") + ", ..."
}

func withTotal(summary string, total int, truncated bool) string {
	if total == 0 {
		return ""
	}
	if truncated {
		return fmt.Sprintf("%s (%d total)", summary, total)
	}
	return summary
}

func collapseCommonPrefix(names []string) (collapsed string, truncated bool, ok bool) {
	if len(names) < 3 {
		return "", false, false
	}

	prefix := longestCommonPrefix(names)
	if len(prefix) < 3 {
		return "", false, false
	}
	prefix, _ = strings.CutSuffix(prefix, "_")
	if len(prefix) < 3 {
		return "", false, false
	}

	suffixes := make([]string, 0, len(names))
	for _, name := range names {
		suffix := strings.TrimPrefix(name, prefix)
		suffix = strings.TrimPrefix(suffix, "_")
		if suffix == "" {
			return "", false, false
		}
		suffixes = append(suffixes, suffix)
	}

	preview := suffixes
	if len(preview) > groupPreviewLimit {
		preview = append([]string{}, preview[:groupPreviewLimit]...)
		truncated = true
	}

	body := strings.Join(preview, ", ")
	if truncated {
		body += ", ..."
	}
	return fmt.Sprintf("%s{%s}", prefix, body), truncated, true
}
