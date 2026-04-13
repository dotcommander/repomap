//go:build !notreesitter

package repomap

import (
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func registerTSTypeScript() {
	tsProvider := func() *tree_sitter.Language {
		return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_typescript.LanguageTypescript()))
	}
	registerTS("typescript", tsProvider, tsQueryTS, tsQueryTSImports)

	tsxProvider := func() *tree_sitter.Language {
		return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_typescript.LanguageTSX()))
	}
	registerTS("tsx", tsxProvider, tsQueryTS, tsQueryTSImports)

	// JavaScript uses the TypeScript grammar (superset).
	registerTS("javascript", tsProvider, tsQueryTS, tsQueryTSImports)
	// JSX uses the TSX grammar.
	registerTS("jsx", tsxProvider, tsQueryTS, tsQueryTSImports)
}

// tsQueryTS finds functions, classes, interfaces, enums, type aliases, and methods.
const tsQueryTS = `
(function_declaration
  name: (identifier) @name) @type

(class_declaration
  name: (type_identifier) @name) @type

(interface_declaration
  name: (type_identifier) @name) @type

(type_alias_declaration
  name: (type_identifier) @name) @type

(enum_declaration
  name: (identifier) @name) @type

(method_definition
  name: (property_identifier) @name) @type

(abstract_method_signature
  name: (property_identifier) @name) @type
`

// tsQueryTSImports finds import statements.
const tsQueryTSImports = `
(import_statement
  source: (string) @path)
`
