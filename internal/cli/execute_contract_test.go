package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteParserContract(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "invalid flag", args: []string{"--not-a-flag"}, want: "unknown flag"},
		{name: "invalid command shape", args: []string{"not-a-command", "extra"}, want: "unexpected argument"},
		{name: "completion rejected", args: []string{"completion"}, want: "no source files found"},
		{name: "version remains unsupported", args: []string{"version"}, want: "no source files found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := executeTest(t, tc.args, io.Discard, io.Discard)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestExecuteHelpAliasAfterArtifactFlag(t *testing.T) {
	t.Parallel()
	artifact := filepath.Join(t.TempDir(), "unused")
	var out bytes.Buffer
	require.NoError(t, executeTest(t, []string{"--artifact", artifact, "help", "find"}, &out, io.Discard))
	assert.Contains(t, out.String(), "repomap find")
	_, err := os.Stat(artifact)
	assert.ErrorIs(t, err, os.ErrNotExist, "metadata help must not create an artifact")
}

func TestExecuteProductionHelpLayout(t *testing.T) {
	t.Parallel()

	var root bytes.Buffer
	require.NoError(t, executeTest(t, []string{"--help"}, &root, io.Discard))
	rootHelp := root.String()
	assert.Contains(t, rootHelp, "Commands:\n  audit")
	assert.Less(t, strings.Index(rootHelp, "Commands:"), strings.Index(rootHelp, "Flags:"), "flags-last layout")

	var audit bytes.Buffer
	require.NoError(t, executeTest(t, []string{"audit", "--help"}, &audit, io.Discard))
	auditHelp := audit.String()
	assert.Contains(t, auditHelp, "Usage: repomap audit <command> [flags]")
	assert.Contains(t, auditHelp, "  surface")
	assert.Contains(t, auditHelp, "    [<directory>]")
	assert.Less(t, strings.Index(auditHelp, "Commands:"), strings.Index(auditHelp, "Flags:"), "nested flags-last layout")

	var surface bytes.Buffer
	require.NoError(t, executeTest(t, []string{"audit", "surface", "--help"}, &surface, io.Discard))
	surfaceHelp := surface.String()
	assert.Contains(t, surfaceHelp, "Usage: repomap audit surface [<directory>] [flags]")
	assert.Contains(t, surfaceHelp, "Report deterministic command, flag, config, schema, route, and output surfaces")
	assert.Less(t, strings.Index(surfaceHelp, "Arguments:"), strings.Index(surfaceHelp, "Flags:"), "leaf flags-last layout")
}

func TestExecuteCompactShorthand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc Main() {}\n"), 0o644))
	var out bytes.Buffer
	require.NoError(t, executeTest(t, []string{"-t64", "-fcompact", dir}, &out, io.Discard))
	assert.Contains(t, out.String(), "Main")
}

func TestExecuteCanceledContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc Main() {}\n"), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := execute(ctx, []string{dir}, io.Discard, io.Discard)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestExitCodePreservesReturnedCommandError(t *testing.T) {
	t.Parallel()
	err := commandExitError{code: 5, err: errors.New("verify failed")}
	assert.Equal(t, 5, ExitCode(err))
	assert.Equal(t, 1, ExitCode(errors.New("ordinary failure")))
}
