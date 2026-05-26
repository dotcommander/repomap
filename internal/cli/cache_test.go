package cli

import (
	"errors"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/require"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestPrintCacheStatusReturnsWriteError(t *testing.T) {
	t.Parallel()

	err := printCacheStatus(failingWriter{}, repomap.CacheStatus{CachePath: "/tmp/repomap-cache"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "write failed")
}
