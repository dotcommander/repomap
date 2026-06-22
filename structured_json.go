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
	Coverage      ParseCoverage    `json:"coverage"`
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
	SymbolRefs     bool     `json:"symbol_refs,omitempty"`
}

// StructuredFile is a machine-readable file block.
type StructuredFile struct {
	Path             string               `json:"path"`
	Handle           string               `json:"handle,omitempty"`
	Language         string               `json:"language,omitempty"`
	CapabilityTier   string               `json:"capability_tier,omitempty"`
	Package          string               `json:"package,omitempty"`
	ImportPath       string               `json:"import_path,omitempty"`
	ParseMethod      string               `json:"parse_method,omitempty"`
	Score            int                  `json:"score"`
	ScoreComponents  map[string]int       `json:"score_components,omitempty"`
	DetailLevel      int                  `json:"detail_level"`
	ImportedBy       int                  `json:"imported_by,omitempty"`
	DependsOn        int                  `json:"depends_on,omitempty"`
	Untested         bool                 `json:"untested,omitempty"`
	Boundaries       []string             `json:"boundaries,omitempty"`
	Imports          []string             `json:"imports,omitempty"`
	RelationEvidence []StructuredEvidence `json:"relation_evidence,omitempty"`
	Symbols          []StructuredSymbol   `json:"symbols,omitempty"`
	OmittedReason    string               `json:"omitted_reason,omitempty"`
}

// StructuredEvidence explains a relationship signal that was used by ranking
// or emitted for structured consumers.
type StructuredEvidence struct {
	Kind          string `json:"kind"`
	EvidenceClass string `json:"evidence_class"`
	Confidence    string `json:"confidence"`
	Detail        string `json:"detail"`
	Caveat        string `json:"caveat,omitempty"`
}

// StructuredSymbol is the machine-readable symbol shape with stable JSON keys.
type StructuredSymbol struct {
	Name        string   `json:"name"`
	Handle      string   `json:"handle,omitempty"`
	FileHandle  string   `json:"file_handle,omitempty"`
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
	Hash        string   `json:"hash,omitempty"`
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
	coverage := m.coverage
	m.mu.RUnlock()

	return BuildStructuredOutput(root, cfg, ranked, tsAvailable, ctagsAvailable, blocklist, coverage)
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
	coverage := m.coverage
	m.mu.RUnlock()

	return BuildStructuredOutput(root, cfg, cloneRanked(ranked), tsAvailable, ctagsAvailable, blocklist, coverage)
}

// BuildStructuredOutput builds the machine-readable output for an already-ranked
// file list. Callers that apply extra score passes, such as --calls, can pass
// that adjusted ranked slice without mutating Map state.
func BuildStructuredOutput(root string, cfg Config, ranked []RankedFile, tsAvailable, ctagsAvailable bool, blocklist *BlocklistConfig, coverage ParseCoverage) StructuredOutput {
	totalFiles, totalSymbols := countTotals(ranked)
	if coverage.FilesScanned == 0 && len(ranked) > 0 {
		coverage = parseCoverageFromRanked(ranked, tsAvailable, ctagsAvailable)
	}
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
			SymbolRefs:     cfg.SymbolRefs,
		},
		Coverage: coverage,
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
		Path:             filepath.ToSlash(f.Path),
		Handle:           FileHandle(filepath.ToSlash(f.Path)),
		Language:         f.Language,
		CapabilityTier:   LanguageCapabilityTier(f.Language),
		Package:          f.Package,
		ImportPath:       f.ImportPath,
		ParseMethod:      f.ParseMethod,
		Score:            f.Score,
		ScoreComponents:  cloneScoreComponents(f.ScoreComponents),
		DetailLevel:      f.DetailLevel,
		ImportedBy:       f.ImportedBy,
		DependsOn:        f.DependsOn,
		Untested:         f.Untested,
		Boundaries:       append([]string(nil), f.Boundaries...),
		Imports:          append([]string(nil), f.Imports...),
		RelationEvidence: structuredRelationEvidence(f),
		Symbols:          structuredSymbolsForFile(filepath.ToSlash(f.Path), f.Symbols),
		OmittedReason:    omitted,
	}
}

func structuredRelationEvidence(f RankedFile) []StructuredEvidence {
	var out []StructuredEvidence
	if f.ImportedBy > 0 {
		if f.Language == "go" {
			out = append(out, StructuredEvidence{
				Kind:          "import_reference",
				EvidenceClass: "import_graph",
				Confidence:    "high",
				Detail:        "Go import path matched scanned package import path",
			})
		} else {
			out = append(out, StructuredEvidence{
				Kind:          "import_reference",
				EvidenceClass: "heuristic",
				Confidence:    "medium",
				Detail:        "Non-Go import string matched scanned file basename",
				Caveat:        "Basename matching can miss aliases and re-exports or collide on common names",
			})
		}
	}
	if f.ScoreComponents[scoreComponentSymbolRefs] > 0 {
		out = append(out, StructuredEvidence{
			Kind:          "symbol_reference",
			EvidenceClass: "heuristic",
			Confidence:    "low",
			Detail:        "Other files mentioned exported symbol names lexically",
			Caveat:        "Lexical references are capped and approximate; use exact callers where available",
		})
	}
	return out
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

func structuredSymbolsForFile(file string, symbols []Symbol) []StructuredSymbol {
	if len(symbols) == 0 {
		return nil
	}
	out := make([]StructuredSymbol, 0, len(symbols))
	for _, s := range symbols {
		out = append(out, StructuredSymbol{
			Name:        s.Name,
			Handle:      SymbolHandle(file, s),
			FileHandle:  FileHandle(file),
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
			Hash:        s.Hash,
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
