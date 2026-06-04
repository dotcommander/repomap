// Package lsp provides an LSP client for code intelligence.
// It communicates with language servers via JSON-RPC 2.0 over stdin/stdout.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// ErrServerDied is returned when the language server process exits unexpectedly.
var ErrServerDied = errors.New("language server died")

// Client is an LSP client connected to a language server process.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	nextID  atomic.Int64
	writeMu sync.Mutex // protects stdin writes
	mu      sync.Mutex // protects pending map
	pending map[int64]chan rpcResult
	done    chan struct{} // closed when readLoop exits

	initDone atomic.Bool
}

// NewClient spawns a language server process and returns a connected client.
func NewClient(ctx context.Context, command string, args ...string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	// Discard gopls stderr — it's chatty and we don't need it.
	cmd.Stderr = io.Discard

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReaderSize(stdout, 64*1024),
		pending: make(map[int64]chan rpcResult),
		done:    make(chan struct{}),
	}

	go c.readLoop()
	return c, nil
}

// Initialize performs the LSP initialize/initialized handshake.
func (c *Client) Initialize(ctx context.Context, rootPath string) error {
	rootURI := pathToURI(rootPath)

	params := InitializeParams{
		RootURI: rootURI,
		Capabilities: ClientCapabilities{
			TextDocument: TextDocumentClientCapabilities{
				Definition: CapabilitySupport{DynamicRegistration: false},
				References: CapabilitySupport{DynamicRegistration: false},
				Hover:      CapabilitySupport{DynamicRegistration: false},
				DocumentSymbol: DocumentSymbolSupport{
					DynamicRegistration:               false,
					HierarchicalDocumentSymbolSupport: true,
				},
			},
		},
	}

	var result InitializeResult
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	if err := c.notify("initialized", struct{}{}); err != nil {
		return fmt.Errorf("initialized: %w", err)
	}

	c.initDone.Store(true)
	return nil
}

// Definition returns definition locations for the symbol at the given position.
func (c *Client) Definition(ctx context.Context, file string, line, col int) ([]Location, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(file)},
		Position:     Position{Line: line, Character: col},
	}

	raw, err := c.callRaw(ctx, "textDocument/definition", params)
	if err != nil {
		return nil, err
	}

	// Response can be Location, []Location, or LocationLink[]
	var locs []Location
	if err := json.Unmarshal(raw, &locs); err == nil && len(locs) > 0 {
		return locs, nil
	}
	var single Location
	if err := json.Unmarshal(raw, &single); err == nil && single.URI != "" {
		return []Location{single}, nil
	}
	var links []LocationLink
	if err := json.Unmarshal(raw, &links); err == nil {
		for _, l := range links {
			locs = append(locs, Location{URI: l.TargetURI, Range: l.TargetRange})
		}
		return locs, nil
	}
	return nil, nil
}

// References returns all reference locations for the symbol at the given position.
func (c *Client) References(ctx context.Context, file string, line, col int) ([]Location, error) {
	params := ReferenceParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: pathToURI(file)},
			Position:     Position{Line: line, Character: col},
		},
		Context: ReferenceContext{IncludeDeclaration: true},
	}

	var locs []Location
	if err := c.call(ctx, "textDocument/references", params, &locs); err != nil {
		return nil, err
	}
	return locs, nil
}

// Hover returns hover information for the symbol at the given position.
func (c *Client) Hover(ctx context.Context, file string, line, col int) (*HoverResult, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(file)},
		Position:     Position{Line: line, Character: col},
	}

	var result HoverResult
	if err := c.call(ctx, "textDocument/hover", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DocumentSymbols returns symbols defined in the given file.
func (c *Client) DocumentSymbols(ctx context.Context, file string) ([]DocumentSymbol, error) {
	params := DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(file)},
	}

	raw, err := c.callRaw(ctx, "textDocument/documentSymbol", params)
	if err != nil {
		return nil, err
	}

	// Detect format: DocumentSymbol[] has "selectionRange", SymbolInformation[] has "location".
	// json.Unmarshal is lenient and succeeds for both, so we probe the raw JSON instead.
	if isSymbolInformationArray(raw) {
		var infos []SymbolInformation
		if err := json.Unmarshal(raw, &infos); err != nil {
			return nil, fmt.Errorf("unmarshal symbol information: %w", err)
		}
		symbols := make([]DocumentSymbol, 0, len(infos))
		for _, si := range infos {
			symbols = append(symbols, DocumentSymbol{
				Name:           si.Name,
				Kind:           si.Kind,
				Range:          si.Location.Range,
				SelectionRange: si.Location.Range,
			})
		}
		return symbols, nil
	}

	var symbols []DocumentSymbol
	if err := json.Unmarshal(raw, &symbols); err != nil {
		return nil, fmt.Errorf("unmarshal document symbols: %w", err)
	}
	return symbols, nil
}

// DidOpen notifies the server that a file has been opened.
func (c *Client) DidOpen(ctx context.Context, file, languageID, content string) error {
	return c.notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        pathToURI(file),
			LanguageID: languageID,
			Version:    1,
			Text:       content,
		},
	})
}

// DidClose notifies the server that a file has been closed.
func (c *Client) DidClose(file string) error {
	return c.notify("textDocument/didClose", DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(file)},
	})
}

// Shutdown sends the shutdown request and exit notification.
func (c *Client) Shutdown(ctx context.Context) error {
	if !c.initDone.Load() {
		return nil
	}
	_ = c.call(ctx, "shutdown", nil, nil)
	_ = c.notify("exit", nil)
	return c.cmd.Wait()
}
