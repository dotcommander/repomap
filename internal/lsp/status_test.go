package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectStatusDetectsMarkerRootsForAvailableServer(t *testing.T) {
	root := t.TempDir()
	writeStatusFile(t, root, "go.mod")
	writeStatusFile(t, root, "main.go")
	withLookPath(t, map[string]bool{"gopls": true})

	report, err := detectStatus(context.Background(), root, defaultStatusMaxDepth)
	require.NoError(t, err)

	require.Len(t, report.Servers, 1)
	assert.Equal(t, "go", report.Servers[0].Language)
	assert.Equal(t, root, report.Servers[0].Root)
	assert.Equal(t, "gopls", report.Servers[0].Command)
	assert.Empty(t, report.Missing)
}

func TestDetectStatusReportsMissingServerForSourceFiles(t *testing.T) {
	root := t.TempDir()
	writeStatusFile(t, root, "src/app.py")
	withLookPath(t, nil)

	report, err := detectStatus(context.Background(), root, defaultStatusMaxDepth)
	require.NoError(t, err)

	require.Len(t, report.Missing, 1)
	assert.Equal(t, "python", report.Missing[0].Language)
	assert.Equal(t, []string{"pylsp", "pyright-langserver"}, report.Missing[0].TriedCommands)
	assert.Equal(t, []string{".py"}, report.Missing[0].FoundExtensions)
	assert.Empty(t, report.Servers)
}

func TestDetectStatusDedupesNestedMarkerRoots(t *testing.T) {
	root := t.TempDir()
	writeStatusFile(t, root, "package.json")
	writeStatusFile(t, root, "packages/app/package.json")
	writeStatusFile(t, root, "packages/app/src/index.ts")
	withLookPath(t, map[string]bool{"typescript-language-server": true})

	report, err := detectStatus(context.Background(), root, defaultStatusMaxDepth)
	require.NoError(t, err)

	require.Len(t, report.Servers, 1)
	assert.Equal(t, "typescript", report.Servers[0].Language)
	assert.Equal(t, root, report.Servers[0].Root)
}

func TestDetectStatusKeepsIndependentMarkerRoots(t *testing.T) {
	root := t.TempDir()
	writeStatusFile(t, root, "apps/a/tsconfig.json")
	writeStatusFile(t, root, "apps/a/src/index.ts")
	writeStatusFile(t, root, "apps/b/tsconfig.json")
	writeStatusFile(t, root, "apps/b/src/index.ts")
	withLookPath(t, map[string]bool{"typescript-language-server": true})

	report, err := detectStatus(context.Background(), root, defaultStatusMaxDepth)
	require.NoError(t, err)

	require.Len(t, report.Servers, 2)
	assert.Equal(t, filepath.Join(root, "apps", "a"), report.Servers[0].Root)
	assert.Equal(t, filepath.Join(root, "apps", "b"), report.Servers[1].Root)
}

func TestDetectStatusIgnoresDependencyDirs(t *testing.T) {
	root := t.TempDir()
	writeStatusFile(t, root, "node_modules/pkg/tsconfig.json")
	writeStatusFile(t, root, "node_modules/pkg/index.ts")
	withLookPath(t, map[string]bool{"typescript-language-server": true})

	report, err := detectStatus(context.Background(), root, defaultStatusMaxDepth)
	require.NoError(t, err)

	assert.Empty(t, report.Servers)
	assert.Empty(t, report.Missing)
}

func writeStatusFile(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("x\n"), 0o644))
}

func withLookPath(t *testing.T, available map[string]bool) {
	t.Helper()
	old := lookPath
	lookPath = func(file string) (string, error) {
		if available[file] {
			return "/bin/" + file, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPath = old })
}
