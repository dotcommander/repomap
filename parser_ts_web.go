//go:build !notreesitter

package repomap

import (
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
)

func registerTSWeb() {
	registerTS("ruby",
		func() *tree_sitter.Language {
			return tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_ruby.Language()))
		},
		tsQueryRuby, tsQueryRubyImports,
	)
}

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
