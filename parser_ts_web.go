//go:build !notreesitter

package repomap

import (
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
)

func registerTSWeb() {
	registerTS("php",
		func() *tree_sitter.Language {
			return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_php.LanguagePHP()))
		},
		tsQueryPHP, tsQueryPHPImports,
	)
	registerTS("ruby",
		func() *tree_sitter.Language {
			return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_ruby.Language()))
		},
		tsQueryRuby, tsQueryRubyImports,
	)
}

// tsQueryPHP finds classes, interfaces, traits, enums, functions, and methods.
const tsQueryPHP = `
(class_declaration
  name: (name) @name) @type

(interface_declaration
  name: (name) @name) @type

(trait_declaration
  name: (name) @name) @type

(enum_declaration
  name: (name) @name) @type

(function_definition
  name: (name) @name) @type

(method_declaration
  name: (name) @name) @type
`

// tsQueryPHPImports finds namespace and use statements.
const tsQueryPHPImports = `
(namespace_definition
  name: (namespace_name) @path)

(namespace_use_clause
  name: (name) @path)
`

// tsQueryRuby finds classes, modules, and method definitions.
const tsQueryRuby = `
(class
  name: (constant) @name) @type

(module
  name: (constant) @name) @type

(method
  name: (identifier) @name) @type

(singleton_method
  name: (identifier) @name) @type
`

// tsQueryRubyImports finds require statements.
const tsQueryRubyImports = `
(call
  method: (identifier) @_method
  arguments: (argument_list (string) @path)
  (#eq? @_method "require"))
`
