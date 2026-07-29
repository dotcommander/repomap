package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dotcommander/repomap"
	"github.com/dotcommander/repomap/internal/serve"
)

const (
	errParse          = -32700
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errServer         = -32000
	errServerDegraded = -32001
)

type serveServer struct {
	root   string
	m      *repomap.Map
	codec  *serve.Codec
	stderr io.Writer
}

type rawRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"`
}

type rpcErrObj struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   rpcErrObj       `json:"error"`
}

type serveCommand struct {
	Directory string `arg:"" optional:"" type:"path" default:"." help:"Directory to serve"`
}

func (c *serveCommand) Run(ctx context.Context, ioctx *commandIO) error {
	absDir, err := filepath.Abs(c.Directory)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	m := repomap.New(absDir, repomap.DefaultConfig())
	fmt.Fprintf(ioctx.stderr, "repomap serve: building map for %s...\n", absDir)
	if err := m.Build(ctx); err != nil {
		return err
	}
	fmt.Fprintln(ioctx.stderr, "repomap serve: ready")
	s := &serveServer{
		root:   absDir,
		m:      m,
		codec:  serve.NewCodec(os.Stdin, ioctx.stdout),
		stderr: ioctx.stderr,
	}
	if err := s.Run(ctx); err != nil {
		return err
	}
	fmt.Fprintln(ioctx.stderr, "repomap serve: shutting down")
	return nil
}

// Run is the main loop. Exits when stdin yields io.EOF or ctx is cancelled.
// Goroutine exit conditions:
//   - readLoop  — io.EOF or scanner error on stdin (producer; closes requestCh)
//   - dispatch  — requestCh closed and drained, or ctx cancelled
func (s *serveServer) Run(ctx context.Context) error {
	requestCh := make(chan rawRequest, 8)
	readDone := make(chan error, 1)
	go func() {
		readDone <- s.readLoop(requestCh)
	}()
	return s.dispatchLoop(ctx, requestCh, readDone)
}

func (s *serveServer) readLoop(requestCh chan<- rawRequest) error {
	defer close(requestCh)
	for {
		var req rawRequest
		if err := s.codec.ReadMessage(&req); err != nil {
			switch {
			case errors.Is(err, io.EOF):
				return nil
			case errors.Is(err, bufio.ErrTooLong):
				// A Scanner cannot continue after an oversized token. Emit the
				// protocol parse error once, then close the request channel so
				// dispatch stops rather than repeatedly reporting the same frame.
				return s.respondErr(nil, errParse, "parse error")
			case isParseError(err):
				if err := s.respondErr(nil, errParse, "parse error"); err != nil {
					return err
				}
				continue
			default:
				return nil
			}
		}
		requestCh <- req
	}
}

func isParseError(err error) bool {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &syntaxErr) || errors.As(err, &typeErr)
}

func (s *serveServer) dispatchLoop(ctx context.Context, requestCh <-chan rawRequest, readDone <-chan error) error {
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case req, ok := <-requestCh:
					if !ok {
						return <-readDone
					}
					if err := s.respondErr(req.ID, errServer, "shutting down"); err != nil {
						return err
					}
				default:
					return nil
				}
			}
		case req, ok := <-requestCh:
			if !ok {
				return <-readDone
			}
			if err := s.handle(ctx, req); err != nil {
				return err
			}
		}
	}
}

func (s *serveServer) handle(ctx context.Context, req rawRequest) error {
	if s.m.Stale() {
		fmt.Fprintf(s.stderr, "repomap serve: rebuilding (stale)\n")
		if err := s.m.Build(ctx); err != nil {
			return s.respondErr(req.ID, errServer, err.Error())
		}
	}

	var (
		result any
		rpcErr *rpcErrObj
	)
	switch req.Method {
	case "map/render":
		result, rpcErr = s.rpcMapRender(req)
	case "map/status":
		result, rpcErr = s.rpcMapStatus(req)
	case "symbol/find":
		result, rpcErr = s.rpcSymbolFind(req)
	case "file/explain":
		result, rpcErr = s.rpcFileExplain(req)
	case "file/context":
		result, rpcErr = s.rpcFileContext(req)
	default:
		return s.respondErr(req.ID, errMethodNotFound, "method not found")
	}
	if rpcErr != nil {
		return s.respondErr(req.ID, rpcErr.Code, rpcErr.Message)
	}
	return s.respond(req.ID, result)
}

func (s *serveServer) respond(id json.RawMessage, result any) error {
	return s.codec.WriteMessage(rpcResponse{JSONRPC: "2.0", ID: normalizeID(id), Result: result})
}

func (s *serveServer) respondErr(id json.RawMessage, code int, msg string) error {
	return s.codec.WriteMessage(rpcErrorResponse{JSONRPC: "2.0", ID: normalizeID(id), Error: rpcErrObj{Code: code, Message: msg}})
}

func normalizeID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	var v any
	if err := json.Unmarshal(id, &v); err != nil {
		return json.RawMessage("null")
	}
	switch v.(type) {
	case nil, string, float64:
		return id
	default:
		return json.RawMessage("null")
	}
}
