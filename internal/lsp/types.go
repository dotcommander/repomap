package lsp

// Minimal LSP protocol types — only what we need for the client.

// Position in a text document (0-based line and character).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location represents a location in a document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// LocationLink is an alternative definition response format.
type LocationLink struct {
	TargetURI   string `json:"targetUri"`
	TargetRange Range  `json:"targetRange"`
}

// TextDocumentIdentifier identifies a document by URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentItem represents a document with content.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentPositionParams is a common base for position-based requests.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// ReferenceParams extends TextDocumentPositionParams with reference context.
type ReferenceParams struct {
	TextDocumentPositionParams
	Context ReferenceContext `json:"context"`
}

// ReferenceContext controls whether the declaration is included in references.
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// HoverResult is the response to textDocument/hover.
type HoverResult struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// MarkupContent represents hover/documentation content.
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// DocumentSymbolParams for textDocument/documentSymbol.
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DocumentSymbol represents a symbol in a document (hierarchical).
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// SymbolInformation is the flat (non-hierarchical) symbol format.
type SymbolInformation struct {
	Name     string     `json:"name"`
	Kind     SymbolKind `json:"kind"`
	Location Location   `json:"location"`
}

// SymbolKind constants.
type SymbolKind int

const (
	SymbolKindFile          SymbolKind = 1
	SymbolKindModule        SymbolKind = 2
	SymbolKindNamespace     SymbolKind = 3
	SymbolKindPackage       SymbolKind = 4
	SymbolKindClass         SymbolKind = 5
	SymbolKindMethod        SymbolKind = 6
	SymbolKindProperty      SymbolKind = 7
	SymbolKindField         SymbolKind = 8
	SymbolKindConstructor   SymbolKind = 9
	SymbolKindEnum          SymbolKind = 10
	SymbolKindInterface     SymbolKind = 11
	SymbolKindFunction      SymbolKind = 12
	SymbolKindVariable      SymbolKind = 13
	SymbolKindConstant      SymbolKind = 14
	SymbolKindString        SymbolKind = 15
	SymbolKindNumber        SymbolKind = 16
	SymbolKindBoolean       SymbolKind = 17
	SymbolKindArray         SymbolKind = 18
	SymbolKindObject        SymbolKind = 19
	SymbolKindKey           SymbolKind = 20
	SymbolKindNull          SymbolKind = 21
	SymbolKindEnumMember    SymbolKind = 22
	SymbolKindStruct        SymbolKind = 23
	SymbolKindEvent         SymbolKind = 24
	SymbolKindOperator      SymbolKind = 25
	SymbolKindTypeParameter SymbolKind = 26
)

var symbolKindNames = map[SymbolKind]string{
	SymbolKindFile:          "file",
	SymbolKindModule:        "module",
	SymbolKindNamespace:     "namespace",
	SymbolKindPackage:       "package",
	SymbolKindClass:         "class",
	SymbolKindMethod:        "method",
	SymbolKindProperty:      "property",
	SymbolKindField:         "field",
	SymbolKindConstructor:   "constructor",
	SymbolKindEnum:          "enum",
	SymbolKindInterface:     "interface",
	SymbolKindFunction:      "function",
	SymbolKindVariable:      "variable",
	SymbolKindConstant:      "constant",
	SymbolKindString:        "string",
	SymbolKindNumber:        "number",
	SymbolKindBoolean:       "boolean",
	SymbolKindArray:         "array",
	SymbolKindObject:        "object",
	SymbolKindKey:           "key",
	SymbolKindNull:          "null",
	SymbolKindEnumMember:    "enum member",
	SymbolKindStruct:        "struct",
	SymbolKindEvent:         "event",
	SymbolKindOperator:      "operator",
	SymbolKindTypeParameter: "type parameter",
}

// String returns the human-readable name of the symbol kind.
func (k SymbolKind) String() string {
	if name, ok := symbolKindNames[k]; ok {
		return name
	}
	return "unknown"
}

// DidOpenTextDocumentParams for textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidCloseTextDocumentParams for textDocument/didClose.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// InitializeParams for the initialize request.
type InitializeParams struct {
	ProcessID    int                `json:"processId,omitempty"`
	RootURI      string             `json:"rootUri"`
	Capabilities ClientCapabilities `json:"capabilities"`
}

// ClientCapabilities describes client capabilities.
type ClientCapabilities struct {
	TextDocument TextDocumentClientCapabilities `json:"textDocument,omitempty"`
}

// TextDocumentClientCapabilities describes text document capabilities.
type TextDocumentClientCapabilities struct {
	Definition     CapabilitySupport     `json:"definition,omitempty"`
	References     CapabilitySupport     `json:"references,omitempty"`
	Hover          CapabilitySupport     `json:"hover,omitempty"`
	DocumentSymbol DocumentSymbolSupport `json:"documentSymbol,omitempty"`
}

// DocumentSymbolSupport describes documentSymbol capabilities.
type DocumentSymbolSupport struct {
	DynamicRegistration               bool `json:"dynamicRegistration"`
	HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport"`
}

// CapabilitySupport is a generic capability flag.
type CapabilitySupport struct {
	DynamicRegistration bool `json:"dynamicRegistration"`
}

// InitializeResult is the response to initialize.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities describes what the server supports.
type ServerCapabilities struct {
	DefinitionProvider     any `json:"definitionProvider,omitempty"`
	ReferencesProvider     any `json:"referencesProvider,omitempty"`
	HoverProvider          any `json:"hoverProvider,omitempty"`
	DocumentSymbolProvider any `json:"documentSymbolProvider,omitempty"`
}
