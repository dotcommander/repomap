//go:build !notreesitter

package repomap

import (
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
)

func registerTSCFamily() {
	registerTS("c",
		func() *tree_sitter.Language {
			return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_c.Language()))
		},
		tsQueryC, tsQueryCImports,
	)
	registerTS("cpp",
		func() *tree_sitter.Language {
			return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_cpp.Language()))
		},
		tsQueryCpp, tsQueryCImports,
	)
}

// tsQueryC finds structs, enums, typedefs, and top-level functions.
const tsQueryC = `
(function_definition
  declarator: (function_declarator
    declarator: (identifier) @name)) @type

(struct_specifier
  name: (type_identifier) @name) @type

(enum_specifier
  name: (type_identifier) @name) @type

(type_definition
  declarator: (type_identifier) @name) @type
`

// tsQueryCpp adds class and namespace to the C queries.
const tsQueryCpp = `
(function_definition
  declarator: (function_declarator
    declarator: (identifier) @name)) @type

(function_definition
  declarator: (function_declarator
    declarator: (qualified_identifier
      name: (identifier) @name))) @type

(class_specifier
  name: (type_identifier) @name) @type

(struct_specifier
  name: (type_identifier) @name) @type

(enum_specifier
  name: (type_identifier) @name) @type

(type_definition
  declarator: (type_identifier) @name) @type

(namespace_definition
  name: (identifier) @name) @type
`

// tsQueryCImports finds #include directives.
const tsQueryCImports = `
(preproc_include
  path: (string_literal) @path)
`
