package serve

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodecReadWrite(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	c := NewCodec(&buf, &buf)
	err := c.WriteMessage(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "x"})
	require.NoError(t, err)

	var got map[string]any
	err = c.ReadMessage(&got)
	require.NoError(t, err)
	require.Equal(t, "2.0", got["jsonrpc"])
	require.Equal(t, float64(1), got["id"])
	require.Equal(t, "x", got["method"])
}

func TestCodecLargeMessage(t *testing.T) {
	t.Parallel()

	want := strings.Repeat("a", 1<<20)
	var buf bytes.Buffer
	c := NewCodec(&buf, &buf)
	err := c.WriteMessage(map[string]any{"payload": want})
	require.NoError(t, err)

	var got map[string]string
	err = c.ReadMessage(&got)
	require.NoError(t, err)
	require.Len(t, got["payload"], len(want))
	require.Equal(t, want, got["payload"])
}

func TestCodecEOF(t *testing.T) {
	t.Parallel()

	c := NewCodec(strings.NewReader(""), io.Discard)
	var v map[string]any
	err := c.ReadMessage(&v)
	require.ErrorIs(t, err, io.EOF)
}

func TestCodecConcurrentWrite(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	c := NewCodec(strings.NewReader(""), &buf)
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := c.WriteMessage(map[string]int{"n": i})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	lines := strings.Split(buf.String(), "\n")
	nonEmpty := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		nonEmpty++
		require.True(t, json.Valid([]byte(line)))
	}
	require.Equal(t, 10, nonEmpty)
}
