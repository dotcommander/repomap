package repomap

// Symbol represents a single extracted symbol from a source file.
type Symbol struct {
	Name        string   // e.g. "Agent", "New", "Run"
	Kind        string   // "function", "method", "struct", "interface", "constant", "variable", "type", "class", "enum"
	Signature   string   // e.g. "(ctx, provider, opts) *Agent" — params + return, no func keyword
	Receiver    string   // e.g. "*Agent" — methods only, empty for functions
	Exported    bool     // true if the symbol is exported (uppercase first letter)
	Dead        bool     // true when exported but no file in the scanned tree imports this file
	Line        int      // 1-based source line number (0 = unknown)
	EndLine     int      // 1-based end line number (0 = unknown, same as Line when unavailable)
	ParamCount  int      // parameter count (funcs/methods); method count (interfaces); 0 otherwise
	ResultCount int      // return value count (funcs/methods only); 0 otherwise
	Implements  []string // interface names this type implements (structs only; Go-module-local)
	Doc         string   `json:"doc,omitempty"` // first-sentence of the Go doc comment (empty if none)
}

// HasFields reports whether the symbol is a struct or interface with
// populated field/method info in its Signature.
func (s Symbol) HasFields() bool {
	return (s.Kind == "struct" || s.Kind == "interface") && s.Signature != "" && s.Signature != "{}"
}

// LineSpan returns the number of lines the symbol spans, or 0 if unknown.
func (s Symbol) LineSpan() int {
	if s.EndLine <= s.Line || s.Line == 0 {
		return 0
	}
	return s.EndLine - s.Line + 1
}

// FileSymbols holds all symbols extracted from a single source file.
type FileSymbols struct {
	Path        string // relative path from project root
	Language    string // language ID
	Package     string // Go package name (empty for non-Go)
	ImportPath  string // Go import path from module (empty for non-Go)
	Symbols     []Symbol
	Imports     []string // import paths (Go) or module names (other)
	ParseMethod string   // "ast", "treesitter", "ctags", or "regex" — signals symbol fidelity
}
