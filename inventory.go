package repomap

import (
	"fmt"
	"strconv"
	"strings"
)

func QueryInventory(cacheDir string, filter string) (string, error) {
	inv := LoadInventory(cacheDir)
	if inv == nil {
		return "No inventory found. Run 'scan' first to build the inventory.", nil
	}
	matched := inv.Files
	if filter != "" {
		op, key, value, err := parseFilter(filter)
		if err != nil {
			return "", fmt.Errorf("invalid filter %q: %w", filter, err)
		}
		var filtered []FileMetrics
		for _, f := range matched {
			if matchesFilter(f, op, key, value) {
				filtered = append(filtered, f)
			}
		}
		matched = filtered
	}
	if len(matched) == 0 {
		return "No files match the filter.", nil
	}
	return formatInventoryTable(matched, ""), nil
}

func parseFilter(filter string) (op, key, value string, err error) {
	for _, c := range []string{">=", "<=", "!=", ">", "<", "="} {
		if idx := strings.Index(filter, c); idx >= 0 {
			return c, strings.TrimSpace(filter[:idx]), strings.TrimSpace(filter[idx+len(c):]), nil
		}
	}
	return "", "", filter, fmt.Errorf("no operator found (expected one of = > < >= <= !=)")
}

func matchesFilter(f FileMetrics, op, key, value string) bool {
	var val int
	switch key {
	case "lines":
		val = f.Lines
	case "imports":
		val = f.Imports
	case "path":
		if op == "=" {
			return strings.Contains(f.Path, value)
		}
		return op == "!=" && !strings.Contains(f.Path, value)
	default:
		return false
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return false
	}
	switch op {
	case ">":
		return val > n
	case ">=":
		return val >= n
	case "<":
		return val < n
	case "<=":
		return val <= n
	case "=":
		return val == n
	default:
		return val != n
	}
}

func formatInventoryTable(files []FileMetrics, header string) string {
	var b strings.Builder
	if header != "" {
		b.WriteString(header)
	}
	b.WriteString("| file | lines | imports | modified |\n")
	b.WriteString("|------|-------|---------|----------|\n")
	for _, f := range files {
		fmt.Fprintf(&b, "| %s | %d | %d | %s |\n", f.Path, f.Lines, f.Imports, f.LastMod)
	}
	return b.String()
}
