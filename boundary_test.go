package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyBoundaries_Basic(t *testing.T) {
	t.Parallel()
	labels, bump := classifyBoundaries([]string{"net/http", "encoding/json"})
	require.Equal(t, []string{"HTTP"}, labels)
	assert.Equal(t, 5, bump)
}

func TestClassifyBoundaries_Multiple(t *testing.T) {
	t.Parallel()
	labels, bump := classifyBoundaries([]string{"net/http", "github.com/jackc/pgx/v5"})
	require.Equal(t, []string{"HTTP", "Postgres"}, labels)
	assert.Equal(t, 10, bump)
}

func TestClassifyBoundaries_Cap(t *testing.T) {
	t.Parallel()
	// HTTP(5) + Postgres(5) + Redis(5) + Kafka(5) = 20 → capped at 15
	imports := []string{
		"net/http",
		"github.com/jackc/pgx/v5",
		"github.com/redis/go-redis/v9",
		"github.com/segmentio/kafka-go",
	}
	labels, bump := classifyBoundaries(imports)
	assert.Equal(t, 4, len(labels), "expected four boundary labels")
	assert.Equal(t, maxBoundaryBump, bump, "bump should be capped at maxBoundaryBump")
}

func TestClassifyBoundaries_Dedupe(t *testing.T) {
	t.Parallel()
	// Both net/http and chi match the HTTP rule — label emitted once, bump counted once.
	labels, bump := classifyBoundaries([]string{"net/http", "github.com/go-chi/chi/v5"})
	require.Equal(t, []string{"HTTP"}, labels)
	assert.Equal(t, 5, bump)
}

func TestClassifyBoundaries_NoMatch(t *testing.T) {
	t.Parallel()
	labels, bump := classifyBoundaries([]string{"encoding/json", "fmt"})
	assert.Empty(t, labels)
	assert.Equal(t, 0, bump)
}

func TestApplyBoundaryBoost(t *testing.T) {
	t.Parallel()

	httpPgx := &FileSymbols{
		Path:     "handler.go",
		Language: "go",
		Imports:  []string{"net/http", "github.com/jackc/pgx/v5"},
		Symbols:  []Symbol{{Name: "Handle", Kind: "function", Exported: true}},
	}
	plain := &FileSymbols{
		Path:     "util.go",
		Language: "go",
		Imports:  []string{"encoding/json", "fmt"},
		Symbols:  []Symbol{{Name: "Format", Kind: "function", Exported: true}},
	}

	ranked := []RankedFile{
		{FileSymbols: httpPgx, Score: 10},
		{FileSymbols: plain, Score: 10},
	}

	applyBoundaryBoost(ranked)

	// HTTP + Postgres = +10
	assert.Equal(t, 20, ranked[0].Score)
	assert.Equal(t, []string{"HTTP", "Postgres"}, ranked[0].Boundaries)

	// no boundary match — score and Boundaries unchanged
	assert.Equal(t, 10, ranked[1].Score)
	assert.Empty(t, ranked[1].Boundaries)
}

func TestRenderDetail_BoundaryLabels(t *testing.T) {
	t.Parallel()

	// Use symbol/path names that don't overlap with boundary label strings,
	// so compact-mode assertions are unambiguous.
	sym := Symbol{Name: "Handle", Kind: "method", Exported: true, Receiver: "*Server"}
	withBoundaries := RankedFile{
		FileSymbols: &FileSymbols{
			Path:     "server/handler.go",
			Language: "go",
			Symbols:  []Symbol{sym},
		},
		DetailLevel: 2,
		Boundaries:  []string{"HTTP", "Postgres"},
	}
	withoutBoundaries := RankedFile{
		FileSymbols: &FileSymbols{
			Path:     "server/handler.go",
			Language: "go",
			Symbols:  []Symbol{sym},
		},
		DetailLevel: 2,
	}

	t.Run("detail mode shows boundary labels", func(t *testing.T) {
		t.Parallel()
		out := formatFileBlockDetail(withBoundaries)
		assert.Contains(t, out, "[HTTP, Postgres]")
	})

	t.Run("verbose mode shows boundary labels", func(t *testing.T) {
		t.Parallel()
		out := formatFileBlockVerbose(withBoundaries)
		assert.Contains(t, out, "[HTTP, Postgres]")
	})

	t.Run("compact mode does not show boundary labels", func(t *testing.T) {
		t.Parallel()
		// withoutBoundaries: same file but no Boundaries set — compact never shows them.
		out := formatFileBlockCompact(withoutBoundaries, nil)
		assert.NotContains(t, out, "HTTP")
		assert.NotContains(t, out, "Postgres")
		// Also verify that even when Boundaries are set, compact does not render them.
		outWithSet := formatFileBlockCompact(withBoundaries, nil)
		assert.NotContains(t, outWithSet, "[HTTP")
	})
}

// TestClassifyBoundaries_NonGoImports verifies that non-Go style import paths
// (TypeScript/Python/Rust package names) do not match Go-only boundary rules
// and return empty labels without panicking. The boundary rules table is
// intentionally Go-centric; future work may add per-language tables.
func TestClassifyBoundaries_NonGoImports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		imports []string
	}{
		{
			name:    "TypeScript_HTTP",
			imports: []string{"express", "fastify", "koa", "hono"},
		},
		{
			name:    "TypeScript_DB",
			imports: []string{"prisma", "@prisma/client", "drizzle-orm", "pg"},
		},
		{
			name:    "Python_HTTP",
			imports: []string{"fastapi", "flask", "django", "starlette"},
		},
		{
			name:    "Python_DB",
			imports: []string{"sqlalchemy", "asyncpg", "psycopg2", "tortoise"},
		},
		{
			name:    "Rust_HTTP",
			imports: []string{"axum", "actix-web", "warp", "rocket"},
		},
		{
			name:    "Rust_DB",
			imports: []string{"sqlx", "diesel", "sea-orm"},
		},
		{
			name:    "empty",
			imports: []string{},
		},
		{
			name:    "nil",
			imports: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			labels, bump := classifyBoundaries(tc.imports)
			assert.Empty(t, labels, "non-Go imports must not match Go boundary rules")
			assert.Equal(t, 0, bump, "non-Go imports must produce zero score bump")
		})
	}
}

// TestApplyBoundaryBoost_NonGoLanguages verifies that applyBoundaryBoost does not
// panic and produces no labels for non-Go files whose imports are language-native
// package names (not Go import paths).
func TestApplyBoundaryBoost_NonGoLanguages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		language string
		imports  []string
	}{
		{
			name:     "TypeScript express+prisma",
			language: "typescript",
			imports:  []string{"express", "prisma"},
		},
		{
			name:     "Python fastapi+sqlalchemy",
			language: "python",
			imports:  []string{"fastapi", "sqlalchemy"},
		},
		{
			name:     "Rust axum+sqlx",
			language: "rust",
			imports:  []string{"axum", "sqlx"},
		},
		{
			name:     "unknown language",
			language: "cobol",
			imports:  []string{"anything"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := &FileSymbols{
				Path:     "app." + tc.language,
				Language: tc.language,
				Imports:  tc.imports,
				Symbols:  []Symbol{{Name: "Handler", Kind: "function", Exported: true}},
			}
			ranked := []RankedFile{{FileSymbols: fs, Score: 10}}
			// Must not panic.
			applyBoundaryBoost(ranked)
			// Non-Go language imports don't match Go boundary rules.
			assert.Empty(t, ranked[0].Boundaries,
				"non-Go language %q must produce no boundary labels", tc.language)
			assert.Equal(t, 10, ranked[0].Score,
				"non-Go language %q score must be unchanged", tc.language)
		})
	}
}

// TestClassifyBoundaries_TableDriven exercises every label in the boundary rules
// table, ensuring each prefix produces the expected label and a positive score bump.
func TestClassifyBoundaries_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		label    string
		imp      string
		wantBump int
	}{
		{"HTTP", "net/http", 5},
		{"HTTP", "github.com/go-chi/chi/v5", 5},
		{"HTTP", "github.com/gin-gonic/gin", 5},
		{"HTTP", "github.com/gorilla/mux", 5},
		{"Postgres", "github.com/jackc/pgx/v5", 5},
		{"Postgres", "database/sql", 5},
		{"Postgres", "github.com/lib/pq", 5},
		{"Redis", "github.com/redis/go-redis/v9", 5},
		{"Redis", "github.com/go-redis/redis/v8", 5},
		{"Kafka", "github.com/segmentio/kafka-go", 5},
		{"Kafka", "github.com/IBM/sarama", 5},
		{"Kafka", "github.com/Shopify/sarama", 5},
		{"gRPC", "google.golang.org/grpc", 5},
		{"Shell", "os/exec", 3},
		{"Crypto", "crypto/tls", 3},
		{"Crypto", "golang.org/x/crypto/bcrypt", 3},
	}

	for _, tc := range tests {
		t.Run(tc.label+"_"+tc.imp, func(t *testing.T) {
			t.Parallel()
			labels, bump := classifyBoundaries([]string{tc.imp})
			require.Contains(t, labels, tc.label,
				"import %q must produce label %q", tc.imp, tc.label)
			assert.Equal(t, tc.wantBump, bump,
				"single %q match must produce bump=%d", tc.label, tc.wantBump)
		})
	}
}

func TestRenderXML_BoundaryAttribute(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Handle", Kind: "function", Exported: true}

	t.Run("boundaries attribute present when set", func(t *testing.T) {
		t.Parallel()
		f := RankedFile{
			FileSymbols: &FileSymbols{
				Path:     "api/handler.go",
				Language: "go",
				Symbols:  []Symbol{sym},
			},
			DetailLevel: 2,
			Boundaries:  []string{"HTTP", "Postgres"},
		}
		files := []RankedFile{f}
		out := FormatXML(files, 0)
		assert.Contains(t, out, `boundaries="HTTP,Postgres"`)
	})

	t.Run("boundaries attribute absent when empty", func(t *testing.T) {
		t.Parallel()
		f := RankedFile{
			FileSymbols: &FileSymbols{
				Path:     "util/format.go",
				Language: "go",
				Symbols:  []Symbol{sym},
			},
			DetailLevel: 2,
		}
		files := []RankedFile{f}
		out := FormatXML(files, 0)
		assert.NotContains(t, out, "boundaries=")
	})
}
