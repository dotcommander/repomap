package repomap

// Symbol represents a single extracted symbol from a source file.
type Symbol struct {
	Name      string // e.g. "Agent", "New", "Run"
	Kind      string // "function", "method", "struct", "interface", "constant", "variable", "type", "class", "enum"
	Signature string // e.g. "(ctx, provider, opts) *Agent" — params + return, no func keyword
	Receiver  string // e.g. "*Agent" — methods only, empty for functions
	Exported  bool   // true if the symbol is exported (uppercase first letter)
	Line      int    // 1-based source line number (0 = unknown)
}

// FileSymbols holds all symbols extracted from a single source file.
type FileSymbols struct {
	Path        string // relative path from project root
	Language    string // language ID
	Package     string // Go package name (empty for non-Go)
	ImportPath  string // Go import path from module (empty for non-Go)
	Symbols     []Symbol
	Imports     []string // import paths (Go) or module names (other)
	ParseMethod string   // "ast", "ctags", or "regex" — signals symbol fidelity
}
