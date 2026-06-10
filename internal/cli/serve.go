package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/dotcommander/repomap"
	"github.com/dotcommander/repomap/internal/serve"
	"github.com/spf13/cobra"
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

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve [directory]",
		Short: "Start a long-lived JSON-RPC 2.0 server on stdin/stdout",
		Long: `Start a long-lived JSON-RPC 2.0 server on stdin/stdout.

The repository map is built once on startup and kept warm. Subsequent queries
skip the scan→parse→rank pipeline unless the map becomes stale. Requests and
responses use NDJSON framing: one JSON-RPC 2.0 object per line.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			m := repomap.New(absDir, repomap.DefaultConfig())
			fmt.Fprintf(cmd.ErrOrStderr(), "repomap serve: building map for %s...\n", absDir)
			if err := m.Build(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "repomap serve: ready\n")

			s := &serveServer{
				root:   absDir,
				m:      m,
				codec:  serve.NewCodec(cmd.InOrStdin(), cmd.OutOrStdout()),
				stderr: cmd.ErrOrStderr(),
			}
			s.Run(cmd.Context())
			fmt.Fprintf(cmd.ErrOrStderr(), "repomap serve: shutting down\n")
			return nil
		},
	}
	return cmd
}

// Run is the main loop. Exits when stdin yields io.EOF or ctx is cancelled.
// Goroutine exit conditions:
//   - readLoop  — io.EOF or scanner error on stdin (producer; closes requestCh)
//   - dispatch  — requestCh closed and drained, or ctx cancelled
func (s *serveServer) Run(ctx context.Context) error {
	requestCh := make(chan rawRequest, 8)
	go s.readLoop(requestCh)
	s.dispatchLoop(ctx, requestCh)
	return nil
}

func (s *serveServer) readLoop(requestCh chan<- rawRequest) {
	defer close(requestCh)
	for {
		var req rawRequest
		if err := s.codec.ReadMessage(&req); err != nil {
			switch {
			case errors.Is(err, io.EOF):
				return
			case isParseError(err):
				s.respondErr(nil, errParse, "parse error")
				continue
			default:
				return
			}
		}
		requestCh <- req
	}
}

func isParseError(err error) bool {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &syntaxErr) || errors.As(err, &typeErr) || errors.Is(err, bufio.ErrTooLong)
}

func (s *serveServer) dispatchLoop(ctx context.Context, requestCh <-chan rawRequest) {
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case req, ok := <-requestCh:
					if !ok {
						return
					}
					s.respondErr(req.ID, errServer, "shutting down")
				default:
					return
				}
			}
		case req, ok := <-requestCh:
			if !ok {
				return
			}
			s.handle(ctx, req)
		}
	}
}

func (s *serveServer) handle(ctx context.Context, req rawRequest) {
	if s.m.Stale() {
		fmt.Fprintf(s.stderr, "repomap serve: rebuilding (stale)\n")
		if err := s.m.Build(ctx); err != nil {
			s.respondErr(req.ID, errServer, err.Error())
			return
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
		s.respondErr(req.ID, errMethodNotFound, "method not found")
		return
	}
	if rpcErr != nil {
		s.respondErr(req.ID, rpcErr.Code, rpcErr.Message)
		return
	}
	s.respond(req.ID, result)
}

func (s *serveServer) respond(id json.RawMessage, result any) {
	_ = s.codec.WriteMessage(rpcResponse{JSONRPC: "2.0", ID: normalizeID(id), Result: result})
}

func (s *serveServer) respondErr(id json.RawMessage, code int, msg string) {
	_ = s.codec.WriteMessage(rpcErrorResponse{JSONRPC: "2.0", ID: normalizeID(id), Error: rpcErrObj{Code: code, Message: msg}})
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
