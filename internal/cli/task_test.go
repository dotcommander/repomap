package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskCommandHelpDefaultsAndValidation(t *testing.T) {
	t.Parallel()
	var help bytes.Buffer
	require.NoError(t, executeTest(t, []string{"task", "--help"}, &help, io.Discard))
	assert.Contains(t, help.String(), "--tokens=4096")
	assert.Contains(t, help.String(), "--consumed")
	for _, args := range [][]string{{"task", "", "."}, {"task", "--tokens=0", "goal", "."}} {
		err := executeTest(t, args, io.Discard, io.Discard)
		require.Error(t, err)
	}
}

func TestTaskCommandJSONAndArtifact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Handler() {}\n"), 0o644))
	var out bytes.Buffer
	require.NoError(t, executeTest(t, []string{"task", "--json", "handler", root}, &out, io.Discard))
	var report map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out.Bytes(), &report))
	assert.Contains(t, report, "targets")

	target := filepath.Join(t.TempDir(), "task.json")
	require.NoError(t, executeTest(t, []string{"--artifact", target, "task", "--json", "handler", root}, io.Discard, io.Discard))
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &report))
}

func TestTaskCommandPropagatesWriterFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Handler() {}\n"), 0o644))
	err := executeTest(t, []string{"task", "--json", "handler", root}, taskFailingWriter{}, io.Discard)
	require.ErrorIs(t, err, errTaskWriter)
}

var errTaskWriter = errors.New("task writer failed")

type taskFailingWriter struct{}

func (taskFailingWriter) Write([]byte) (int, error) { return 0, errTaskWriter }
