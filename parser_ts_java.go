//go:build !notreesitter

package repomap

import (
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

func registerTSJava() {
	registerTS("java",
		func() *tree_sitter.Language {
			return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_java.Language()))
		},
		tsQueryJava, tsQueryJavaImports,
	)
}

// tsQueryJava finds classes, interfaces, enums, records, and methods.
const tsQueryJava = `
(class_declaration
  name: (identifier) @name) @type

(interface_declaration
  name: (identifier) @name) @type

(enum_declaration
  name: (identifier) @name) @type

(record_declaration
  name: (identifier) @name) @type

(method_declaration
  name: (identifier) @name) @type

(constructor_declaration
  name: (identifier) @name) @type
`

// tsQueryJavaImports finds import declarations.
const tsQueryJavaImports = `
(import_declaration
  path: (scoped_identifier) @path)
`
