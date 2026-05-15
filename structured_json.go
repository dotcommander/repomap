package repomap

import (
	"encoding/json"
	"path/filepath"
)

// StructuredOutput is the machine-readable repository map format.
type StructuredOutput struct {
	SchemaVersion int              `json:"schema_version"`
	Root          string           `json:"root"`
	Totals        StructuredTotals `json:"totals"`
	Config        StructuredConfig `json:"config"`
	Warnings      []string         `json:"warnings,omitempty"`
	Files         []StructuredFile `json:"files"`
}

// StructuredTotals records unbudgeted repository totals.
type StructuredTotals struct {
	Files   int `json:"files"`
	Symbols int `json:"symbols"`
}

// StructuredConfig records the inputs that materially affect map selection.
type StructuredConfig struct {
	MaxTokens      int      `json:"max_tokens"`
	MaxTokensNoCtx int      `json:"max_tokens_no_ctx"`
	Intent         string   `json:"intent,omitempty"`
	ConsumedPaths  []string `json:"consumed_paths,omitempty"`
}

// StructuredFile is a machine-readable file block.
type StructuredFile struct {
	Path            string             `json:"path"`
	Language        string             `json:"language,omitempty"`
	Package         string             `json:"package,omitempty"`
	ImportPath      string             `json:"import_path,omitempty"`
	ParseMethod     string             `json:"parse_method,omitempty"`
	Score           int                `json:"score"`
	ScoreComponents map[string]int     `json:"score_components,omitempty"`
	DetailLevel     int                `json:"detail_level"`
	ImportedBy      int                `json:"imported_by,omitempty"`
	DependsOn       int                `json:"depends_on,omitempty"`
	Untested        bool               `json:"untested,omitempty"`
	Boundaries      []string           `json:"boundaries,omitempty"`
	Imports         []string           `json:"imports,omitempty"`
	Symbols         []StructuredSymbol `json:"symbols,omitempty"`
	OmittedReason   string             `json:"omitted_reason,omitempty"`
}

// StructuredSymbol is the machine-readable symbol shape with stable JSON keys.
type StructuredSymbol struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Signature   string   `json:"signature,omitempty"`
	Receiver    string   `json:"receiver,omitempty"`
	Exported    bool     `json:"exported,omitempty"`
	Dead        bool     `json:"dead,omitempty"`
	Line        int      `json:"line,omitempty"`
	EndLine     int      `json:"end_line,omitempty"`
	ParamCount  int      `json:"param_count,omitempty"`
	ResultCount int      `json:"result_count,omitempty"`
	Implements  []string `json:"implements,omitempty"`
	Doc         string   `json:"doc,omitempty"`
}

// StructuredJSON returns the structured map encoded as indented JSON.
func (m *Map) StructuredJSON() ([]byte, error) {
	return json.MarshalIndent(m.StructuredOutput(), "", "  ")
}

// StructuredOutput returns a structured snapshot of the built map.
func (m *Map) StructuredOutput() StructuredOutput {
	m.mu.RLock()
	ranked := cloneRanked(m.ranked)
	root := m.root
	cfg := m.config
	tsAvailable := m.tsAvailable
	ctagsAvailable := m.ctagsAvailable
	blocklist := m.blocklist
	m.mu.RUnlock()

	return BuildStructuredOutput(root, cfg, ranked, tsAvailable, ctagsAvailable, blocklist)
}

// StructuredOutputForRanked returns structured output for an adjusted ranked
// slice while reusing this Map's config, root, diagnostics, and file overrides.
func (m *Map) StructuredOutputForRanked(ranked []RankedFile) StructuredOutput {
	m.mu.RLock()
	root := m.root
	cfg := m.config
	tsAvailable := m.tsAvailable
	ctagsAvailable := m.ctagsAvailable
	blocklist := m.blocklist
	m.mu.RUnlock()

	return BuildStructuredOutput(root, cfg, cloneRanked(ranked), tsAvailable, ctagsAvailable, blocklist)
}

// BuildStructuredOutput builds the machine-readable output for an already-ranked
// file list. Callers that apply extra score passes, such as --calls, can pass
// that adjusted ranked slice without mutating Map state.
func BuildStructuredOutput(root string, cfg Config, ranked []RankedFile, tsAvailable, ctagsAvailable bool, blocklist *BlocklistConfig) StructuredOutput {
	totalFiles, totalSymbols := countTotals(ranked)
	if cfg.MaxTokens > 0 {
		ranked = BudgetFiles(cloneRanked(ranked), cfg.MaxTokens, blocklist)
	}

	out := StructuredOutput{
		SchemaVersion: 1,
		Root:          root,
		Totals:        StructuredTotals{Files: totalFiles, Symbols: totalSymbols},
		Config: StructuredConfig{
			MaxTokens:      cfg.MaxTokens,
			MaxTokensNoCtx: cfg.MaxTokensNoCtx,
			Intent:         cfg.Intent,
			ConsumedPaths:  append([]string(nil), cfg.ConsumedPaths...),
		},
		Warnings: structuredWarnings(tsAvailable, ctagsAvailable),
		Files:    make([]StructuredFile, 0, len(ranked)),
	}

	for _, f := range ranked {
		out.Files = append(out.Files, structuredFile(f, omittedReason(f, cfg.MaxTokens, blocklist)))
	}
	return out
}

func structuredWarnings(tsAvailable, ctagsAvailable bool) []string {
	var warnings []string
	if !tsAvailable {
		warnings = append(warnings, "tree-sitter unavailable; non-Go parsing may use lower-fidelity fallbacks")
	}
	if !ctagsAvailable {
		warnings = append(warnings, "ctags unavailable; parser fallback options are reduced")
	}
	return warnings
}

func structuredFile(f RankedFile, omitted string) StructuredFile {
	return StructuredFile{
		Path:            filepath.ToSlash(f.Path),
		Language:        f.Language,
		Package:         f.Package,
		ImportPath:      f.ImportPath,
		ParseMethod:     f.ParseMethod,
		Score:           f.Score,
		ScoreComponents: cloneScoreComponents(f.ScoreComponents),
		DetailLevel:     f.DetailLevel,
		ImportedBy:      f.ImportedBy,
		DependsOn:       f.DependsOn,
		Untested:        f.Untested,
		Boundaries:      append([]string(nil), f.Boundaries...),
		Imports:         append([]string(nil), f.Imports...),
		Symbols:         structuredSymbols(f.Symbols),
		OmittedReason:   omitted,
	}
}

func omittedReason(f RankedFile, maxTokens int, cfg *BlocklistConfig) string {
	if f.DetailLevel >= 0 {
		return ""
	}
	if cfg != nil {
		if level, ok := cfg.MatchFileOverride(f.Path); ok && level < 0 {
			return "file_override"
		}
	}
	if maxTokens > 0 {
		return "budget"
	}
	return "omitted"
}

func structuredSymbols(symbols []Symbol) []StructuredSymbol {
	if len(symbols) == 0 {
		return nil
	}
	out := make([]StructuredSymbol, 0, len(symbols))
	for _, s := range symbols {
		out = append(out, StructuredSymbol{
			Name:        s.Name,
			Kind:        s.Kind,
			Signature:   s.Signature,
			Receiver:    s.Receiver,
			Exported:    s.Exported,
			Dead:        s.Dead,
			Line:        s.Line,
			EndLine:     s.EndLine,
			ParamCount:  s.ParamCount,
			ResultCount: s.ResultCount,
			Implements:  append([]string(nil), s.Implements...),
			Doc:         s.Doc,
		})
	}
	return out
}

func cloneRanked(in []RankedFile) []RankedFile {
	out := make([]RankedFile, len(in))
	for i, f := range in {
		out[i] = f
		out[i].ScoreComponents = cloneScoreComponents(f.ScoreComponents)
		if f.FileSymbols != nil {
			cp := *f.FileSymbols
			cp.Symbols = append([]Symbol(nil), f.Symbols...)
			cp.Imports = append([]string(nil), f.Imports...)
			out[i].FileSymbols = &cp
		}
	}
	return out
}

func cloneScoreComponents(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
