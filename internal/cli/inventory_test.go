package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInventoryCommandPostgres(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "database"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "migrations"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "database", "connection.go"), []byte(`package database

import "github.com/jackc/pgx/v5"

func Open() {
	_ = pgx.QueryResultFormats{}
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "database", "connection_test.go"), []byte(`package database

import "testing"

func TestOpen(t *testing.T) {
	Open()
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "migrations", "001_schema.sql"), []byte("create table users(id int);\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "postgres.md"), []byte("PostgreSQL schema notes\n"), 0o644))

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"inventory", "--boundary", "Postgres", "--json", root})

	require.NoError(t, cmd.Execute())

	var report inventoryReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &report))
	assert.Equal(t, "Postgres", report.Boundary)
	assert.Contains(t, report.Constructors, "internal/database/connection.go")
	assert.Contains(t, report.Tests, "internal/database/connection_test.go")
	assert.Contains(t, report.Migrations, "migrations/001_schema.sql")
	assert.Contains(t, report.Docs, "docs/postgres.md")
}
