package repomap

import (
	"fmt"
	"strings"
)

// FormatMap formats ranked files into a token-budgeted text representation.
// maxTokens controls the output size (estimated as len(text)/4).
// cfg may be nil — nil means no file-level detail overrides.
// Returns empty string if no files have symbols.
// When verbose is true, shows all symbols without summarization.
// When detail is true, shows signatures for funcs/methods and fields for structs.
func FormatMap(files []RankedFile, maxTokens int, verbose, detail bool, cfg *BlocklistConfig) string {
	totalFiles, totalSymbols := countTotals(files)
	if totalFiles == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprint(&b, buildHeader(files, totalFiles, totalSymbols))

	if verbose {
		for _, f := range files {
			if len(f.Symbols) == 0 {
				fmt.Fprint(&b, formatFileHeaderOnly(f))
				continue
			}
			if detail {
				fmt.Fprint(&b, formatFileBlockDetail(f))
			} else {
				fmt.Fprint(&b, formatFileBlockVerbose(f))
			}
		}
		return b.String()
	}

	// Budget mode: assign detail levels, then render.
	files = BudgetFiles(files, maxTokens, cfg)

	var headerOnly []string
	shownFiles := 0
	for _, f := range files {
		if f.DetailLevel < 0 {
			continue
		}
		if f.DetailLevel == 0 && len(f.Symbols) == 0 && f.Tag == "" {
			headerOnly = append(headerOnly, f.Path)
			shownFiles++
			continue
		}
		fmt.Fprint(&b, f.formatDetail())
		shownFiles++
	}

	if len(headerOnly) > 0 {
		fmt.Fprint(&b, formatCollapsedPaths(headerOnly))
	}

	if shownFiles < totalFiles {
		omitted := totalFiles - shownFiles
		fmt.Fprintf(&b, "(%d files omitted — increase -t or use -f compact)\n", omitted)
	}

	return b.String()
}

// FormatMapCompact formats ranked files into the lean orientation mode:
// path + exported symbol names only, NO signatures, NO godoc, NO struct fields.
// Budget is applied using compactCost so more files fit vs. the enriched default.
// cfg may be nil — nil means no file-level detail overrides.
// Returns empty string if no files have symbols.
func FormatMapCompact(files []RankedFile, maxTokens int, cfg *BlocklistConfig) string {
	totalFiles, totalSymbols := countTotals(files)
	if totalFiles == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprint(&b, buildHeader(files, totalFiles, totalSymbols))

	// Budget mode using compact cost — more files fit vs. enriched default.
	files = BudgetFilesCompact(files, maxTokens, cfg)

	var headerOnly []string
	shownFiles := 0
	for _, f := range files {
		if f.DetailLevel < 0 {
			continue
		}
		if f.DetailLevel == 0 && len(f.Symbols) == 0 && f.Tag == "" {
			headerOnly = append(headerOnly, f.Path)
			shownFiles++
			continue
		}
		switch f.DetailLevel {
		case 0:
			fmt.Fprint(&b, formatFileHeaderOnly(f))
		case 1:
			fmt.Fprint(&b, formatFileBlockSummary(f))
		default:
			fmt.Fprint(&b, formatFileBlockLean(f))
		}
		shownFiles++
	}

	if len(headerOnly) > 0 {
		fmt.Fprint(&b, formatCollapsedPaths(headerOnly))
	}

	if shownFiles < totalFiles {
		omitted := totalFiles - shownFiles
		fmt.Fprintf(&b, "(%d files omitted — increase -t)\n", omitted)
	}

	return b.String()
}

// formatDetail renders the file at its assigned DetailLevel.
// DetailLevel 2 and 3 both dispatch to formatFileBlockDefault (enriched default).
// DetailLevel 3 aliasing DetailLevel 2 is intentional for v0.7.0;
// distinction deferred to v0.8.0 (e.g. unexported fields at level 3).
func (f RankedFile) formatDetail() string {
	switch f.DetailLevel {
	case 0:
		return formatFileHeaderOnly(f)
	case 1:
		return formatFileBlockSummary(f)
	case 2, 3:
		return formatFileBlockDefault(f)
	default:
		return ""
	}
}
