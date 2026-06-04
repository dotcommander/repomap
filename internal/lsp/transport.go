package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 transport
// ---------------------------------------------------------------------------

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"` // pointer: null → nil (notification), 0 → valid
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *jsonrpcError) Error() string {
	return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message)
}

type rpcResult struct {
	Data json.RawMessage
	Err  error
}

func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	raw, err := c.callRaw(ctx, method, params)
	if err != nil {
		return err
	}
	if result != nil && len(raw) > 0 {
		return json.Unmarshal(raw, result)
	}
	return nil
}

func (c *Client) callRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	ch := make(chan rpcResult, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.send(req); err != nil {
		return nil, fmt.Errorf("send %s: %w", method, err)
	}

	select {
	case result := <-ch:
		return result.Data, result.Err
	case <-c.done:
		return nil, ErrServerDied
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Client) notify(method string, params any) error {
	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.send(msg)
}

func (c *Client) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(data)
	return err
}

func (c *Client) readLoop() {
	defer close(c.done)

	for {
		// Read headers
		var contentLength int
		for {
			line, err := c.stdout.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "Content-Length:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
				contentLength, _ = strconv.Atoi(val)
			}
		}

		if contentLength == 0 {
			continue
		}

		// Read body
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(c.stdout, body); err != nil {
			return
		}

		// Parse response
		var resp jsonrpcResponse
		if json.Unmarshal(body, &resp) != nil {
			continue
		}

		// Skip notifications (id is null/absent)
		if resp.ID == nil {
			continue
		}

		c.mu.Lock()
		ch, ok := c.pending[*resp.ID]
		c.mu.Unlock()

		if !ok {
			continue
		}

		if resp.Error != nil {
			ch <- rpcResult{Err: resp.Error}
			continue
		}

		ch <- rpcResult{Data: resp.Result}
	}
}
