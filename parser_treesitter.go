//go:build !notreesitter

package repomap

import (
	"context"
	"fmt"
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

type treeSitterRuntime struct {
	mu        sync.Mutex
	languages map[string]*tree_sitter.Language
	pending   map[string]*tsLanguageInit
	parsers   map[string][]*tree_sitter.Parser
	disposed  bool
}

type tsLanguageInit struct {
	done chan struct{}
	lang *tree_sitter.Language
	err  error
}

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

func newTreeSitterRuntime() *treeSitterRuntime {
	return &treeSitterRuntime{
		languages: make(map[string]*tree_sitter.Language),
		pending:   make(map[string]*tsLanguageInit),
		parsers:   make(map[string][]*tree_sitter.Parser),
	}
}

func (rt *treeSitterRuntime) language(langID string) (*tree_sitter.Language, error) {
	rt.mu.Lock()
	if rt.disposed {
		rt.mu.Unlock()
		return nil, fmt.Errorf("tree-sitter runtime disposed")
	}
	if lang := rt.languages[langID]; lang != nil {
		rt.mu.Unlock()
		return lang, nil
	}
	if init := rt.pending[langID]; init != nil {
		rt.mu.Unlock()
		<-init.done
		return init.lang, init.err
	}
	provider, ok := tsRegistry[langID]
	if !ok {
		rt.mu.Unlock()
		return nil, fmt.Errorf("tree-sitter language %q not registered", langID)
	}
	init := &tsLanguageInit{done: make(chan struct{})}
	rt.pending[langID] = init
	rt.mu.Unlock()

	lang := provider()
	if lang == nil {
		init.err = fmt.Errorf("tree-sitter language %q provider returned nil", langID)
	} else {
		init.lang = lang
	}

	rt.mu.Lock()
	delete(rt.pending, langID)
	if init.err == nil && !rt.disposed {
		rt.languages[langID] = init.lang
	}
	rt.mu.Unlock()
	close(init.done)
	return init.lang, init.err
}

func (rt *treeSitterRuntime) acquireParser(langID string) (*tree_sitter.Parser, *tree_sitter.Language, error) {
	lang, err := rt.language(langID)
	if err != nil {
		return nil, nil, err
	}

	rt.mu.Lock()
	if rt.disposed {
		rt.mu.Unlock()
		return nil, nil, fmt.Errorf("tree-sitter runtime disposed")
	}
	var parser *tree_sitter.Parser
	pool := rt.parsers[langID]
	if len(pool) > 0 {
		parser = pool[len(pool)-1]
		rt.parsers[langID] = pool[:len(pool)-1]
	}
	rt.mu.Unlock()

	if parser == nil {
		parser = tree_sitter.NewParser()
	}
	parser.Reset()
	if err := parser.SetLanguage(lang); err != nil {
		parser.Close()
		rt.invalidateLanguage(langID)
		return nil, nil, err
	}
	return parser, lang, nil
}

func (rt *treeSitterRuntime) releaseParser(langID string, parser *tree_sitter.Parser) {
	if parser == nil {
		return
	}
	parser.Reset()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.disposed {
		parser.Close()
		return
	}
	rt.parsers[langID] = append(rt.parsers[langID], parser)
}

func (rt *treeSitterRuntime) invalidateLanguage(langID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.languages, langID)
}

func (rt *treeSitterRuntime) close() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.disposed = true
	for langID, parsers := range rt.parsers {
		for _, parser := range parsers {
			parser.Close()
		}
		delete(rt.parsers, langID)
	}
	rt.languages = nil
}

// parseWithTreeSitter parses a single file using tree-sitter.
// Custom parsers (registered via registerTSCustom) take precedence over the
// generic extractSymbols/extractImports path.
// Returns nil if the language has no tree-sitter grammar registered.
func parseWithTreeSitter(content []byte, lang string, relPath string) *FileSymbols {
	rt := newTreeSitterRuntime()
	defer rt.close()
	return rt.parse(content, lang, relPath)
}

func (rt *treeSitterRuntime) parse(content []byte, lang string, relPath string) *FileSymbols {
	if custom, ok := tsCustomParsers[lang]; ok {
		return custom(content, relPath)
	}
	parser, tsLang, err := rt.acquireParser(lang)
	if err != nil {
		return nil
	}
	defer rt.releaseParser(lang, parser)

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
		ParseMethod: "tree_sitter",
	}

	extractSymbols(content, root, tsLang, fs)
	extractImports(content, root, tsLang, lang, fs)
	extractCallSites(content, root, tsLang, lang, fs)

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
		typeNode := tsCaptureNode(m, q, "type")
		if receiver == "" && typeNode != nil {
			receiver = tsEnclosingReceiver(typeNode, source)
		}
		if receiver != "" && symKind == "function" {
			symKind = "method"
		}

		line := uint(0)
		endLine := uint(0)
		if nameNode := tsCaptureNode(m, q, "name"); nameNode != nil {
			line = nameNode.StartPosition().Row + 1
		}
		// Use @type node for end line — it wraps the full declaration.
		if typeNode != nil {
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

func tsEnclosingReceiver(n *tree_sitter.Node, source []byte) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "class_declaration", "class_definition", "interface_declaration",
			"interface_definition", "trait_item", "struct_item", "impl_item":
			if name := tsNodeName(p, source); name != "" {
				return name
			}
		}
	}
	return ""
}

func tsNodeName(n *tree_sitter.Node, source []byte) string {
	if n == nil {
		return ""
	}
	if name := n.ChildByFieldName("name"); name != nil {
		return name.Utf8Text(source)
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		child := n.Child(i)
		switch child.Kind() {
		case "identifier", "type_identifier":
			return child.Utf8Text(source)
		}
	}
	return ""
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
	case "method_definition", "method_declaration", "abstract_method_signature", "method_signature":
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
	rt := newTreeSitterRuntime()
	defer rt.close()
	return m.parseTreeSitterFileWithRuntime(rt, fi)
}

func (m *Map) parseTreeSitterFileWithRuntime(rt *treeSitterRuntime, fi FileInfo) *FileSymbols {
	absPath := m.absPath(fi.Path)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	return rt.parse(content, fi.Language, fi.Path)
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

	rt := newTreeSitterRuntime()
	defer rt.close()
	parsed := parallelParse(tsFiles, func(fi FileInfo) *FileSymbols {
		return m.parseTreeSitterFileWithRuntime(rt, fi)
	})
	return parsed, fallbackFiles
}
