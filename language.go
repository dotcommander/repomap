package repomap

// languageDef defines a supported language with its file extension and regex parser.
// This is the single source of truth for language support.
// To add a new language: add an entry here, then optionally add a tree-sitter
// registration in parser_ts_*.go.
type languageDef struct {
	ID     string
	Ext    string
	Parser parserFunc // nil for languages without regex support (Go uses AST)
}

var languageDefs = []languageDef{
	{"go", ".go", nil},
	{"typescript", ".ts", parseTS},
	{"tsx", ".tsx", parseTS},
	{"javascript", ".js", parseTS},
	{"jsx", ".jsx", parseTS},
	{"python", ".py", parsePython},
	{"rust", ".rs", parseRust},
	{"c", ".c", parseC},
	{"c", ".h", parseC},
	{"cpp", ".cpp", parseC},
	{"cpp", ".cc", parseC},
	{"java", ".java", parseJava},
	{"ruby", ".rb", parseRuby},
	{"php", ".php", parsePHP},
	{"lua", ".lua", nil},
	{"zig", ".zig", nil},
	{"swift", ".swift", nil},
	{"kotlin", ".kt", nil},
}

var (
	supportedExts = buildExtMap(languageDefs)
	langParsers   = buildParserMap(languageDefs)
)

func buildExtMap(defs []languageDef) map[string]string {
	m := make(map[string]string, len(defs))
	for _, d := range defs {
		m[d.Ext] = d.ID
	}
	return m
}

func buildParserMap(defs []languageDef) map[string]parserFunc {
	m := make(map[string]parserFunc)
	for _, d := range defs {
		if d.Parser != nil {
			m[d.ID] = d.Parser
		}
	}
	return m
}

// LanguageFor returns the language ID for a file extension, or "" if unsupported.
func LanguageFor(ext string) string {
	return supportedExts[ext]
}
