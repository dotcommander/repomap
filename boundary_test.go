package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyBoundaries_Basic(t *testing.T) {
	t.Parallel()
	labels, bump := classifyBoundaries("go", []string{"net/http", "encoding/json"})
	require.Equal(t, []string{"HTTP"}, labels)
	assert.Equal(t, 5, bump)
}

func TestClassifyBoundaries_Multiple(t *testing.T) {
	t.Parallel()
	labels, bump := classifyBoundaries("go", []string{"net/http", "github.com/jackc/pgx/v5"})
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
	labels, bump := classifyBoundaries("go", imports)
	assert.Equal(t, 4, len(labels), "expected four boundary labels")
	assert.Equal(t, maxBoundaryBump, bump, "bump should be capped at maxBoundaryBump")
}

func TestClassifyBoundaries_Dedupe(t *testing.T) {
	t.Parallel()
	// Both net/http and chi match the HTTP rule — label emitted once, bump counted once.
	labels, bump := classifyBoundaries("go", []string{"net/http", "github.com/go-chi/chi/v5"})
	require.Equal(t, []string{"HTTP"}, labels)
	assert.Equal(t, 5, bump)
}

func TestClassifyBoundaries_NoMatch(t *testing.T) {
	t.Parallel()
	labels, bump := classifyBoundaries("go", []string{"encoding/json", "fmt"})
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
