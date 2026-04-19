//go:build !notreesitter

package repomap

import (
	_ "embed"
	"strings"
	"sync"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

//go:embed php_tags.scm
var phpTagsQuery string

// phpQueryOnce guards the compiled query.
var phpQueryOnce sync.Once

// phpCompiledQuery is the package-level compiled PHP query (compile once, reuse).
var phpCompiledQuery *tree_sitter.Query

// phpLang holds the PHP language instance for query compilation.
var phpLang *tree_sitter.Language

func registerTSPHP() {
	provider := func() *tree_sitter.Language {
		return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_php.LanguagePHP()))
	}
	// Initialise the shared language instance used by the query compiler.
	phpLang = provider()
	registerTSCustom("php", provider, parsePHPWithTreeSitter)
}

// phpMatchRecord is an intermediate representation built during the match walk.
// Signature is set by each per-kind builder.
type phpMatchRecord struct {
	kind      string
	name      string
	signature string
	receiver  string
	exported  bool
	line      int
	endLine   int
	startByte uint
}

// parsePHPWithTreeSitter is the custom PHP parser. It replaces the generic
// extractSymbols path so it can use named captures (class.name, method.name,
// property.name, const.name, doc, doc.target) and the adjacency-based PHPDoc
// pairing.
func parsePHPWithTreeSitter(content []byte, relPath string) *FileSymbols {
	parser := tree_sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(phpLang); err != nil {
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

	// Compile the query once across all calls (thread-safe).
	phpQueryOnce.Do(func() {
		q, err := tree_sitter.NewQuery(phpLang, phpTagsQuery)
		if err == nil {
			phpCompiledQuery = q
		}
	})
	if phpCompiledQuery == nil {
		return nil
	}

	fs := &FileSymbols{
		Path:        relPath,
		Language:    "php",
		ParseMethod: "treesitter",
	}

	// docByTarget maps a doc.target node's start byte to its raw comment text.
	// Populated when we see a @doc + @doc.target pair.
	docByTarget := make(map[uint]string)

	var matches []phpMatchRecord

	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()

	qmatches := cursor.Matches(phpCompiledQuery, root, content)
	for {
		m := qmatches.Next()
		if m == nil {
			break
		}

		// PHPDoc adjacency pair — collect and continue.
		if docNode := phpCaptureNode(m, phpCompiledQuery, "doc"); docNode != nil {
			if targetNode := phpCaptureNode(m, phpCompiledQuery, "doc.target"); targetNode != nil {
				raw := string(content[docNode.StartByte():docNode.EndByte()])
				docByTarget[targetNode.StartByte()] = raw
			}
			continue
		}

		if rec, ok := phpDispatchMatch(m, phpCompiledQuery, content); ok {
			matches = append(matches, rec)
		}
	}

	// Constructor promotion post-pass: walk method matches named __construct and
	// emit a property symbol for each property_promotion_parameter child.
	// Runs after the main loop so the constructor method symbol itself is intact.
	matches = phpAppendPromotedProperties(matches, phpCompiledQuery, root, content)

	// Build Symbols, attaching any paired PHPDoc.
	for _, pm := range matches {
		doc := ""
		if raw, ok := docByTarget[pm.startByte]; ok {
			doc = phpFirstSentence(phpStripDocblock(raw))
		}
		fs.Symbols = append(fs.Symbols, Symbol{
			Name:      pm.name,
			Kind:      pm.kind,
			Signature: pm.signature,
			Receiver:  pm.receiver,
			Exported:  pm.exported,
			Line:      pm.line,
			EndLine:   pm.endLine,
			Doc:       doc,
		})
	}

	return fs
}

// phpAppendPromotedProperties runs a secondary cursor pass over the tree to
// find all __construct methods and extract their promoted parameters as
// property symbols. The constructor method symbol itself is left intact.
func phpAppendPromotedProperties(matches []phpMatchRecord, q *tree_sitter.Query, root *tree_sitter.Node, source []byte) []phpMatchRecord {
	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()

	qmatches := cursor.Matches(q, root, source)
	for {
		m := qmatches.Next()
		if m == nil {
			break
		}
		methodNode := phpCaptureNode(m, q, "method")
		if methodNode == nil {
			continue
		}
		nameNode := phpCaptureNode(m, q, "method.name")
		if nameNode == nil || nameNode.Utf8Text(source) != "__construct" {
			continue
		}
		receiver := phpEnclosingClass(methodNode, source)
		promoted := phpPromotedPropertiesFromConstructor(methodNode, receiver, source)
		matches = append(matches, promoted...)
	}
	return matches
}

// phpDispatchMatch routes a query match to the appropriate per-kind handler.
// Returns the populated record and true when the match produces a symbol.
func phpDispatchMatch(m *tree_sitter.QueryMatch, q *tree_sitter.Query, source []byte) (phpMatchRecord, bool) {
	// Class — extended signature with extends/implements.
	if classNode := phpCaptureNode(m, q, "class"); classNode != nil {
		name := phpCaptureText(m, q, "class.name", source)
		if name == "" {
			return phpMatchRecord{}, false
		}
		return phpMatchRecord{
			kind:      "class",
			name:      name,
			signature: phpClassSignature(classNode, source),
			exported:  true,
			line:      int(classNode.StartPosition().Row) + 1,
			endLine:   int(classNode.EndPosition().Row) + 1,
			startByte: classNode.StartByte(),
		}, true
	}

	// Method — visibility + modifiers + params + return type.
	if methodNode := phpCaptureNode(m, q, "method"); methodNode != nil {
		name := phpCaptureText(m, q, "method.name", source)
		if name == "" {
			return phpMatchRecord{}, false
		}
		_, visibility := phpCollectModifiers(methodNode, source)
		return phpMatchRecord{
			kind:      "method",
			name:      name,
			signature: phpMethodSignature(methodNode, source),
			receiver:  phpEnclosingClass(methodNode, source),
			exported:  phpVisibilityToExported(visibility),
			line:      int(methodNode.StartPosition().Row) + 1,
			endLine:   int(methodNode.EndPosition().Row) + 1,
			startByte: methodNode.StartByte(),
		}, true
	}

	// Property — modifiers + type + $name [= default].
	if propNode := phpCaptureNode(m, q, "property"); propNode != nil {
		nameNode := phpCaptureNode(m, q, "property.name")
		if nameNode == nil {
			return phpMatchRecord{}, false
		}
		name := nameNode.Utf8Text(source)
		// variable_name includes the $ — strip it for the symbol Name field so
		// it matches idiomatic identifier usage (Signature retains it).
		name = strings.TrimPrefix(name, "$")
		_, visibility := phpCollectModifiers(propNode, source)
		return phpMatchRecord{
			kind:      "property",
			name:      name,
			signature: phpPropertySignature(propNode, source),
			receiver:  phpEnclosingClass(propNode, source),
			exported:  phpVisibilityToExported(visibility),
			line:      int(propNode.StartPosition().Row) + 1,
			endLine:   int(propNode.EndPosition().Row) + 1,
			startByte: propNode.StartByte(),
		}, true
	}

	// Const — visibility + optional type + NAME = value.
	if constNode := phpCaptureNode(m, q, "const"); constNode != nil {
		name := phpCaptureText(m, q, "const.name", source)
		if name == "" {
			return phpMatchRecord{}, false
		}
		_, visibility := phpCollectModifiers(constNode, source)
		return phpMatchRecord{
			kind:      "const",
			name:      name,
			signature: phpConstSignature(constNode, source),
			receiver:  phpEnclosingClass(constNode, source),
			exported:  phpVisibilityToExported(visibility),
			line:      int(constNode.StartPosition().Row) + 1,
			endLine:   int(constNode.EndPosition().Row) + 1,
			startByte: constNode.StartByte(),
		}, true
	}

	// Interface — name + optional extends clause.
	if ifaceNode := phpCaptureNode(m, q, "interface"); ifaceNode != nil {
		name := phpCaptureText(m, q, "interface.name", source)
		if name == "" {
			return phpMatchRecord{}, false
		}
		return phpMatchRecord{
			kind:      "interface",
			name:      name,
			signature: phpInterfaceSignature(ifaceNode, source),
			exported:  true,
			line:      int(ifaceNode.StartPosition().Row) + 1,
			endLine:   int(ifaceNode.EndPosition().Row) + 1,
			startByte: ifaceNode.StartByte(),
		}, true
	}

	// Trait — name only.
	if traitNode := phpCaptureNode(m, q, "trait"); traitNode != nil {
		name := phpCaptureText(m, q, "trait.name", source)
		if name == "" {
			return phpMatchRecord{}, false
		}
		return phpMatchRecord{
			kind:      "trait",
			name:      name,
			signature: phpTraitSignature(traitNode, source),
			exported:  true,
			line:      int(traitNode.StartPosition().Row) + 1,
			endLine:   int(traitNode.EndPosition().Row) + 1,
			startByte: traitNode.StartByte(),
		}, true
	}

	// Enum — name + optional backing type + optional implements clause.
	if enumNode := phpCaptureNode(m, q, "enum"); enumNode != nil {
		name := phpCaptureText(m, q, "enum.name", source)
		if name == "" {
			return phpMatchRecord{}, false
		}
		return phpMatchRecord{
			kind:      "enum",
			name:      name,
			signature: phpEnumSignature(enumNode, source),
			exported:  true,
			line:      int(enumNode.StartPosition().Row) + 1,
			endLine:   int(enumNode.EndPosition().Row) + 1,
			startByte: enumNode.StartByte(),
		}, true
	}

	// Enum case — backed or unbacked.
	if caseNode := phpCaptureNode(m, q, "case"); caseNode != nil {
		name := phpCaptureText(m, q, "case.name", source)
		if name == "" {
			return phpMatchRecord{}, false
		}
		return phpMatchRecord{
			kind:      "case",
			name:      name,
			signature: phpCaseSignature(caseNode, source),
			receiver:  phpEnclosingClass(caseNode, source),
			exported:  true,
			line:      int(caseNode.StartPosition().Row) + 1,
			endLine:   int(caseNode.EndPosition().Row) + 1,
			startByte: caseNode.StartByte(),
		}, true
	}

	// Free function — full signature (params + return type).
	if fnNode := phpCaptureNode(m, q, "function"); fnNode != nil {
		name := phpCaptureText(m, q, "function.name", source)
		if name == "" {
			return phpMatchRecord{}, false
		}
		return phpMatchRecord{
			kind:      "function",
			name:      name,
			signature: phpFunctionSignature(fnNode, source),
			exported:  true,
			line:      int(fnNode.StartPosition().Row) + 1,
			endLine:   int(fnNode.EndPosition().Row) + 1,
			startByte: fnNode.StartByte(),
		}, true
	}

	return phpMatchRecord{}, false
}

// phpCaptureNode returns the first node for a named capture in a query match.
func phpCaptureNode(m *tree_sitter.QueryMatch, q *tree_sitter.Query, name string) *tree_sitter.Node {
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

// phpCaptureText returns the source text for a named capture in a query match.
func phpCaptureText(m *tree_sitter.QueryMatch, q *tree_sitter.Query, name string, source []byte) string {
	n := phpCaptureNode(m, q, name)
	if n == nil {
		return ""
	}
	return n.Utf8Text(source)
}
