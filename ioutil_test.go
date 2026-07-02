package repomap

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAtomicWriteFileConcurrent(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "target.json")
	payloadA := bytes.Repeat([]byte("a"), 1024)
	payloadB := bytes.Repeat([]byte("b"), 2048)
	errCh := make(chan error, 400)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for range 200 {
			if err := atomicWriteFile(target, payloadA, 0o644); err != nil {
				errCh <- err
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for range 200 {
			if err := atomicWriteFile(target, payloadB, 0o644); err != nil {
				errCh <- err
				return
			}
		}
	}()

	done := make(chan struct{})
	violations := make(chan string, 100)

	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			data, err := os.ReadFile(target)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				violations <- "unexpected read error: " + err.Error()
				continue
			}
			if len(data) == 1024 && data[0] == 'a' {
				continue
			}
			if len(data) == 2048 && data[0] == 'b' {
				continue
			}
			violations <- "torn read"
		}
	}()

	wg.Wait()
	close(done)

	close(errCh)
	close(violations)

	for err := range errCh {
		t.Fatalf("write error: %v", err)
	}

	for v := range violations {
		t.Errorf("reader violation: %s", v)
	}

	// Final file must be one of the two payloads.
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	if !bytes.Equal(data, payloadA) && !bytes.Equal(data, payloadB) {
		t.Fatalf("final file has unexpected content (len=%d)", len(data))
	}
}

func TestAtomicWriteFilePerm(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "perm-target.json")
	data := []byte("hello")
	err := atomicWriteFile(target, data, 0o600)
	require.NoError(t, err)

	info, err := os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
}
