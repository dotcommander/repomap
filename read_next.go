package repomap

// ReadNextItem is a bounded source range worth reading next.
type ReadNextItem struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Reason    string `json:"reason"`
}

const (
	defaultReadNextMaxLines = 80
	defaultReadNextBefore   = 2
	defaultReadNextAfter    = 24
)

func readNextRange(file string, startLine, endLine int, reason string) ReadNextItem {
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	maxEnd := startLine + defaultReadNextMaxLines - 1
	if endLine > maxEnd {
		endLine = maxEnd
	}
	return ReadNextItem{
		File:      file,
		StartLine: startLine,
		EndLine:   endLine,
		Reason:    reason,
	}
}

func readNextAround(file string, line int, reason string) ReadNextItem {
	if line < 1 {
		line = 1
	}
	return readNextRange(file, line-defaultReadNextBefore, line+defaultReadNextAfter, reason)
}

func dedupeReadNext(items []ReadNextItem, limit int) ([]ReadNextItem, string) {
	if len(items) == 0 {
		return nil, ""
	}
	seen := make(map[ReadNextItem]struct{}, len(items))
	out := make([]ReadNextItem, 0, len(items))
	for _, item := range items {
		if item.File == "" || item.Reason == "" {
			continue
		}
		if item.StartLine < 1 {
			item.StartLine = 1
		}
		if item.EndLine < item.StartLine {
			item.EndLine = item.StartLine
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if limit > 0 && len(out) > limit {
		return out[:limit], "showing read-next ranges truncated by cap"
	}
	return out, ""
}
