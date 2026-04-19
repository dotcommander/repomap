//go:build !notreesitter

package repomap

import (
	"context"
	"os"
	"sync"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// tsLangProvider returns a tree-sitter Language for the given language ID.
type tsLangProvider func() *tree_sitter.Language

// tsRegistry maps language IDs to their tree-sitter language providers.
// Populated by register* functions in per-language files, called from init().
var tsRegistry map[string]tsLangProvider

// tsAvailable indicates whether tree-sitter parsing is usable.
var tsAvailable bool

// tsOnce guards the availability check.
var tsOnce sync.Once

// TreeSitterAvailable reports whether tree-sitter parsing is available.
func TreeSitterAvailable() bool {
	tsOnce.Do(func() {
		tsAvailable = len(tsRegistry) > 0
	})
	return tsAvailable
}

// tsSupportedLanguages returns the set of language IDs with tree-sitter grammars.
func tsSupportedLanguages() map[string]bool {
	langs := make(map[string]bool, len(tsRegistry))
	for id := range tsRegistry {
		langs[id] = true
	}
	return langs
}

// tsSymbolQueries maps language IDs to S-expression queries for finding symbols.
var tsSymbolQueries map[string]string

// tsImportQueries maps language IDs to S-expression queries for finding imports.
var tsImportQueries map[string]string

// tsCustomParsers maps language IDs to custom full-file parsers.
// A custom parser bypasses the generic extractSymbols/extractImports path and
// owns the entire FileSymbols construction. Register via registerTSCustom.
var tsCustomParsers map[string]func(content []byte, relPath string) *FileSymbols

// registerTS adds a language to all tree-sitter registries.
func registerTS(lang string, provider tsLangProvider, symQ, impQ string) {
	tsRegistry[lang] = provider
	tsSymbolQueries[lang] = symQ
	tsImportQueries[lang] = impQ
}

// registerTSCustom registers a language that uses a custom parser instead of
// the generic query + extractSymbols path. The language is still added to
// tsRegistry (so it's dispatched by parseTreeSitterFiles) but the custom
// parser takes precedence in parseWithTreeSitter.
func registerTSCustom(lang string, provider tsLangProvider, parser func(content []byte, relPath string) *FileSymbols) {
	tsRegistry[lang] = provider
	tsCustomParsers[lang] = parser
}

func init() {
	tsRegistry = make(map[string]tsLangProvider)
	tsSymbolQueries = make(map[string]string)
	tsImportQueries = make(map[string]string)
	tsCustomParsers = make(map[string]func(content []byte, relPath string) *FileSymbols)

	registerTSCFamily()
	registerTSJava()
	registerTSPHP()
	registerTSPython()
	registerTSRust()
	registerTSTypeScript()
	registerTSWeb()
}

// parseWithTreeSitter parses a single file using tree-sitter.
// Custom parsers (registered via registerTSCustom) take precedence over the
// generic extractSymbols/extractImports path.
// Returns nil if the language has no tree-sitter grammar registered.
func parseWithTreeSitter(content []byte, lang string, relPath string) *FileSymbols {
	if custom, ok := tsCustomParsers[lang]; ok {
		return custom(content, relPath)
	}

	provider, ok := tsRegistry[lang]
	if !ok {
		return nil
	}

	tsLang := provider()
	if tsLang == nil {
		return nil
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(tsLang); err != nil {
		return nil
	}

	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return nil
	}

	fs := &FileSymbols{
		Path:        relPath,
		Language:    lang,
		ParseMethod: "treesitter",
	}

	extractSymbols(content, root, tsLang, fs)
	extractImports(content, root, tsLang, lang, fs)

	return fs
}

// runTSQuery executes a tree-sitter query and calls handle for each match.
func runTSQuery(source []byte, root *tree_sitter.Node, lang *tree_sitter.Language, queryStr string, handle func(m *tree_sitter.QueryMatch, q *tree_sitter.Query)) {
	q, err := tree_sitter.NewQuery(lang, queryStr)
	if err != nil {
		return
	}
	defer q.Close()

	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(q, root, source)
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		handle(m, q)
	}
}

// extractSymbols runs a language-specific S-expression query to find
// top-level symbol definitions.
func extractSymbols(source []byte, root *tree_sitter.Node, lang *tree_sitter.Language, fs *FileSymbols) {
	queryStr := tsSymbolQueries[fs.Language]
	if queryStr == "" {
		return
	}
	runTSQuery(source, root, lang, queryStr, func(m *tree_sitter.QueryMatch, q *tree_sitter.Query) {
		name := tsCaptureText(m, q, "name", source)
		if name == "" {
			return
		}

		symKind := tsSymbolKind(m, q)
		receiver := tsCaptureText(m, q, "receiver", source)

		line := uint(0)
		endLine := uint(0)
		if nameNode := tsCaptureNode(m, q, "name"); nameNode != nil {
			line = nameNode.StartPosition().Row + 1
		}
		// Use @type node for end line — it wraps the full declaration.
		if typeNode := tsCaptureNode(m, q, "type"); typeNode != nil {
			endLine = typeNode.EndPosition().Row + 1
		}

		fs.Symbols = append(fs.Symbols, Symbol{
			Name:     name,
			Kind:     symKind,
			Receiver: receiver,
			Exported: true,
			Line:     int(line),
			EndLine:  int(endLine),
		})
	})
}

// extractImports finds import statements for a given language.
func extractImports(source []byte, root *tree_sitter.Node, lang *tree_sitter.Language, langID string, fs *FileSymbols) {
	queryStr := tsImportQueries[langID]
	if queryStr == "" {
		return
	}
	runTSQuery(source, root, lang, queryStr, func(m *tree_sitter.QueryMatch, q *tree_sitter.Query) {
		if imp := tsCaptureText(m, q, "path", source); imp != "" {
			fs.Imports = append(fs.Imports, imp)
		}
	})
}

// tsCaptureText returns the source text for a named capture in a match.
func tsCaptureText(m *tree_sitter.QueryMatch, q *tree_sitter.Query, name string, source []byte) string {
	n := tsCaptureNode(m, q, name)
	if n == nil {
		return ""
	}
	return n.Utf8Text(source)
}

// tsCaptureNode returns the Node for a named capture in a match.
func tsCaptureNode(m *tree_sitter.QueryMatch, q *tree_sitter.Query, name string) *tree_sitter.Node {
	idx, ok := q.CaptureIndexForName(name)
	if !ok {
		return nil
	}
	nodes := m.NodesForCaptureIndex(idx)
	if len(nodes) == 0 {
		return nil
	}
	return &nodes[0]
}

// tsSymbolKind derives the symbol kind from the @type node's tree-sitter kind.
func tsSymbolKind(m *tree_sitter.QueryMatch, q *tree_sitter.Query) string {
	if n := tsCaptureNode(m, q, "type"); n != nil {
		return tsKindToSymbolKind(n.Kind())
	}
	return "other"
}

// tsKindToSymbolKind maps tree-sitter node kinds to repomap symbol kinds.
func tsKindToSymbolKind(tsKind string) string {
	switch tsKind {
	case "function_declaration", "function_definition", "function_item",
		"arrow_function", "generator_function_declaration":
		return "function"
	case "method_definition", "method_declaration":
		return "method"
	case "class_declaration", "class_definition":
		return "class"
	case "interface_declaration", "interface_definition":
		return "interface"
	case "struct", "struct_item":
		return "struct"
	case "enum_declaration", "enum_definition", "enum_item":
		return "enum"
	case "type_alias_declaration", "type_declaration":
		return "type"
	case "const_declaration", "const_item":
		return "const"
	case "static_item":
		return "variable"
	case "impl_item":
		return "impl"
	case "trait_item":
		return "trait"
	case "record_declaration":
		return "class"
	default:
		return tsKind
	}
}

// parseTreeSitterFile reads and parses a single file with tree-sitter.
func (m *Map) parseTreeSitterFile(fi FileInfo) *FileSymbols {
	absPath := m.absPath(fi.Path)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	return parseWithTreeSitter(content, fi.Language, fi.Path)
}

// parseTreeSitterFiles parses all non-Go files with tree-sitter.
// Returns parsed results and files that need regex fallback.
func (m *Map) parseTreeSitterFiles(ctx context.Context, files []FileInfo) ([]*FileSymbols, []FileInfo) {
	supported := tsSupportedLanguages()

	// Caller (parseNonGoFiles) already filters Go files out.
	var tsFiles, fallbackFiles []FileInfo
	for _, fi := range files {
		if supported[fi.Language] {
			tsFiles = append(tsFiles, fi)
		} else {
			fallbackFiles = append(fallbackFiles, fi)
		}
	}

	if len(tsFiles) == 0 {
		return nil, fallbackFiles
	}

	parsed := parallelParse(tsFiles, m.parseTreeSitterFile)
	return parsed, fallbackFiles
}
