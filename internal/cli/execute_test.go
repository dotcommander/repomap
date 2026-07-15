package cli

import (
	"io"
	"testing"
)

func executeTest(t *testing.T, args []string, stdout, stderr io.Writer) error {
	t.Helper()
	return execute(t.Context(), args, stdout, stderr)
}
