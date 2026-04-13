//go:build !notreesitter

package repomap

import (
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

func registerTSPython() {
	registerTS("python",
		func() *tree_sitter.Language {
			return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_python.Language()))
		},
		tsQueryPython, tsQueryPythonImports,
	)
}

// tsQueryPython finds functions and classes.
const tsQueryPython = `
(function_definition
  name: (identifier) @name) @type

(class_definition
  name: (identifier) @name) @type
`

// tsQueryPythonImports finds import statements.
const tsQueryPythonImports = `
(import_statement
  name: (dotted_name) @path)

(import_from_statement
  module_name: (dotted_name) @path)
`
