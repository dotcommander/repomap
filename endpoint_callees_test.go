package repomap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerCallees(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := `package app

import "net/http"

func getUser(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	user := db.FindUser(id)
	render.JSON(w, user)
}
`
	path := filepath.Join(dir, "handlers.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	// getUser spans lines 5..9 (declaration through closing brace).
	got, err := handlerCallees(path, 5, 9)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"parseID", "db.FindUser", "render.JSON"}, got)
}
