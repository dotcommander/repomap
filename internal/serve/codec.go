// Package serve implements the NDJSON JSON-RPC 2.0 framing layer for
// `repomap serve`. One JSON object per line; no Content-Length headers.
package serve

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
)

// Codec is a single-connection NDJSON framing layer.
// ReadMessage is NOT safe for concurrent use; it belongs to the read goroutine.
// WriteMessage is protected by writeMu and is safe for concurrent use.
type Codec struct {
	scanner *bufio.Scanner
	enc     *json.Encoder
	writeMu sync.Mutex
}

// NewCodec wraps r and w in an NDJSON codec. The scanner accepts lines up to
// 4 MB (64 KB initial buffer).
func NewCodec(r io.Reader, w io.Writer) *Codec {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return &Codec{scanner: sc, enc: json.NewEncoder(w)}
}

// ReadMessage reads one JSON-RPC message from the reader. Returns io.EOF when
// the input stream is exhausted. Not safe for concurrent use.
func (c *Codec) ReadMessage(v any) error {
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return err
		}
		return io.EOF
	}
	return json.Unmarshal(c.scanner.Bytes(), v)
}

// WriteMessage serialises v as a single JSON line. Safe for concurrent use.
func (c *Codec) WriteMessage(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.enc.Encode(v) // Encode appends '\n'
}
