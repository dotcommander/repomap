//go:build !notreesitter

package repomap

import (
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

func registerTSRust() {
	registerTS("rust",
		func() *tree_sitter.Language {
			return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_rust.Language()))
		},
		tsQueryRust, tsQueryRustImports,
	)
}

// tsQueryRust finds functions, structs, enums, traits, impls, types, and consts.
const tsQueryRust = `
(function_item
  name: (identifier) @name) @type

(struct_item
  name: (type_identifier) @name) @type

(enum_item
  name: (type_identifier) @name) @type

(trait_item
  name: (type_identifier) @name) @type

(impl_item
  type: (type_identifier) @name) @type

(type_item
  name: (type_identifier) @name) @type

(const_item
  name: (identifier) @name) @type

(static_item
  name: (identifier) @name) @type
`

// tsQueryRustImports finds use declarations.
const tsQueryRustImports = `
(use_declaration
  argument: (_) @path)
`
