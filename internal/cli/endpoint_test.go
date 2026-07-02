package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runEndpointCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := newEndpointCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// writeEndpointFixture writes a temp dir containing one chi route and one
// net/http route registration.
func writeEndpointFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := `package app

import "net/http"

func getUser(w http.ResponseWriter, r *http.Request)    {}
func createUser(w http.ResponseWriter, r *http.Request) {}

func register(r Router, mux *http.ServeMux) {
	r.Get("/users/{id}", getUser)
	mux.HandleFunc("POST /users", createUser)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "routes.go"), []byte(src), 0o644))
	return dir
}

func TestEndpointCmd_ListMode(t *testing.T) {
	t.Parallel()
	dir := writeEndpointFixture(t)
	out, err := runEndpointCmd(t, dir)
	require.NoError(t, err)
	// Phase B fills the table; against the Phase A stub this prints
	// "no routes found", so these assertions fail red by design.
	assert.Contains(t, out, "getUser")
	assert.Contains(t, out, "/users/{id}")
	assert.Contains(t, out, "chi")
	assert.Contains(t, out, "createUser")
}

func TestEndpointCmd_SinglePatternBundle(t *testing.T) {
	t.Parallel()
	dir := writeEndpointFixture(t)
	out, err := runEndpointCmd(t, "GET /users/{id}", dir)
	// Phase C resolves the bundle; against the Phase A stub Map.Endpoint
	// returns an error, so this fails red by design.
	require.NoError(t, err)
	assert.Contains(t, out, "getUser")
	assert.Contains(t, out, "/users/{id}")
}

func TestEndpointCmd_NegativeNoRoutes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	out, err := runEndpointCmd(t, dir)
	require.NoError(t, err)
	assert.Equal(t, "no routes found", strings.TrimSpace(out))
}
