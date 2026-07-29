package repomap

import (
	"errors"
	"testing"
)

var errDryRunWrite = errors.New("dry-run write failed")

type dryRunFailWriter struct{}

func (dryRunFailWriter) Write([]byte) (int, error) {
	return 0, errDryRunWrite
}

func TestPrintDryRunReturnsWriteError(t *testing.T) {
	t.Parallel()

	err := printDryRun(dryRunFailWriter{}, []CommitGroup{{SuggestedMsg: "feat: test"}}, ExecuteOptions{})
	if !errors.Is(err, errDryRunWrite) {
		t.Fatalf("printDryRun error = %v, want %v", err, errDryRunWrite)
	}
}
