package repomap

// languageDef defines a supported language with its file extension and regex parser.
// This is the single source of truth for language support.
// To add a new language: add an entry here, then optionally add a tree-sitter
// registration in parser_ts_*.go.
//
// BoundaryRules lists import-prefix rules for boundary classification, in the
// order labels will be emitted. Nil = no boundary detection for that language.
type languageDef struct {
	ID            string
	Ext           string
	Parser        parserFunc     // nil for languages without regex support (Go uses AST)
	BoundaryRules []boundaryRule // ordered; nil = no boundary detection (e.g. C/C++)
}

var languageDefs = []languageDef{
	{
		ID:     "go",
		Ext:    ".go",
		Parser: nil,
		BoundaryRules: []boundaryRule{
			{Label: "HTTP", Prefixes: []string{"net/http", "github.com/go-chi/chi", "github.com/gin-gonic/gin", "github.com/gorilla/mux"}, ScoreBump: 5},
			{Label: "Postgres", Prefixes: []string{"github.com/jackc/pgx", "database/sql", "github.com/lib/pq"}, ScoreBump: 5},
			{Label: "Redis", Prefixes: []string{"github.com/redis/", "github.com/go-redis/"}, ScoreBump: 5},
			{Label: "Kafka", Prefixes: []string{"github.com/segmentio/kafka-go", "github.com/IBM/sarama", "github.com/Shopify/sarama"}, ScoreBump: 5},
			{Label: "gRPC", Prefixes: []string{"google.golang.org/grpc"}, ScoreBump: 5},
			{Label: "Shell", Prefixes: []string{"os/exec"}, ScoreBump: 3},
			{Label: "Crypto", Prefixes: []string{"crypto/", "golang.org/x/crypto"}, ScoreBump: 3},
		},
	},
	{ID: "typescript", Ext: ".ts", Parser: parseTS, BoundaryRules: []boundaryRule{
		{Label: "HTTP", Prefixes: []string{"express", "hono", "fastify", "koa", "next"}, ScoreBump: 5},
		{Label: "DB", Prefixes: []string{"prisma", "knex", "pg", "mongoose", "typeorm"}, ScoreBump: 5},
	}},
	{ID: "tsx", Ext: ".tsx", Parser: parseTS, BoundaryRules: []boundaryRule{
		{Label: "HTTP", Prefixes: []string{"express", "hono", "fastify", "koa", "next"}, ScoreBump: 5},
		{Label: "DB", Prefixes: []string{"prisma", "knex", "pg", "mongoose", "typeorm"}, ScoreBump: 5},
	}},
	{ID: "javascript", Ext: ".js", Parser: parseTS, BoundaryRules: []boundaryRule{
		{Label: "HTTP", Prefixes: []string{"express", "hono", "fastify", "koa", "next"}, ScoreBump: 5},
		{Label: "DB", Prefixes: []string{"prisma", "knex", "pg", "mongoose", "typeorm"}, ScoreBump: 5},
	}},
	{ID: "jsx", Ext: ".jsx", Parser: parseTS, BoundaryRules: []boundaryRule{
		{Label: "HTTP", Prefixes: []string{"express", "hono", "fastify", "koa", "next"}, ScoreBump: 5},
		{Label: "DB", Prefixes: []string{"prisma", "knex", "pg", "mongoose", "typeorm"}, ScoreBump: 5},
	}},
	{ID: "python", Ext: ".py", Parser: parsePython, BoundaryRules: []boundaryRule{
		{Label: "HTTP", Prefixes: []string{"fastapi", "flask", "django", "starlette"}, ScoreBump: 5},
		{Label: "DB", Prefixes: []string{"sqlalchemy", "psycopg", "tortoise", "peewee"}, ScoreBump: 5},
	}},
	{ID: "rust", Ext: ".rs", Parser: parseRust, BoundaryRules: []boundaryRule{
		{Label: "HTTP", Prefixes: []string{"actix", "axum", "warp", "rocket"}, ScoreBump: 5},
		{Label: "DB", Prefixes: []string{"sqlx", "diesel", "sea-orm"}, ScoreBump: 5},
	}},
	{ID: "c", Ext: ".c", Parser: parseC, BoundaryRules: nil},
	{ID: "c", Ext: ".h", Parser: parseC, BoundaryRules: nil},
	{ID: "cpp", Ext: ".cpp", Parser: parseC, BoundaryRules: nil},
	{ID: "cpp", Ext: ".cc", Parser: parseC, BoundaryRules: nil},
	{ID: "java", Ext: ".java", Parser: parseJava, BoundaryRules: []boundaryRule{
		{Label: "HTTP", Prefixes: []string{"spring", "javax.servlet", "jakarta.servlet"}, ScoreBump: 5},
		{Label: "DB", Prefixes: []string{"jdbc", "hibernate", "mybatis"}, ScoreBump: 5},
	}},
	{ID: "ruby", Ext: ".rb", Parser: parseRuby, BoundaryRules: nil},
	{ID: "php", Ext: ".php", Parser: parsePHP, BoundaryRules: nil},
	{ID: "lua", Ext: ".lua", Parser: nil, BoundaryRules: nil},
	{ID: "zig", Ext: ".zig", Parser: nil, BoundaryRules: nil},
	{ID: "swift", Ext: ".swift", Parser: nil, BoundaryRules: nil},
	{ID: "kotlin", Ext: ".kt", Parser: nil, BoundaryRules: nil},
}

var (
	supportedExts     = buildExtMap(languageDefs)
	langParsers       = buildParserMap(languageDefs)
	langBoundaryRules = buildBoundaryRulesMap(languageDefs)
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

// buildBoundaryRulesMap indexes the first BoundaryRules entry per language ID.
// Multiple defs share the same ID (e.g. "c" covers .c and .h); rules are
// identical across extensions so the first non-nil entry wins.
func buildBoundaryRulesMap(defs []languageDef) map[string][]boundaryRule {
	m := make(map[string][]boundaryRule, len(defs))
	for _, d := range defs {
		if _, seen := m[d.ID]; !seen {
			m[d.ID] = d.BoundaryRules // nil entries are valid (no detection)
		}
	}
	return m
}

// LanguageFor returns the language ID for a file extension, or "" if unsupported.
func LanguageFor(ext string) string {
	return supportedExts[ext]
}
