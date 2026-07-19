package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/dotcommander/repomap/internal/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMainExecution(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := cli.Execute(context.Background(), []string{"--help"}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "repomap")
}
