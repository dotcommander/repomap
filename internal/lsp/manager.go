package lsp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ServerConfig describes how to start a language server.
type ServerConfig struct {
	Command string
	Args    []string
}

// defaultServers maps language ID to known language server configurations.
var defaultServers = map[string][]ServerConfig{
	"go":         {{Command: "gopls"}},
	"typescript": {{Command: "typescript-language-server", Args: []string{"--stdio"}}},
	"javascript": {{Command: "typescript-language-server", Args: []string{"--stdio"}}},
	"python":     {{Command: "pylsp"}, {Command: "pyright-langserver", Args: []string{"--stdio"}}},
	"rust":       {{Command: "rust-analyzer"}},
	"c":          {{Command: "clangd"}},
	"cpp":        {{Command: "clangd"}},
	"java":       {{Command: "jdtls"}},
	"lua":        {{Command: "lua-language-server"}},
	"zig":        {{Command: "zls"}},
}

// extToLanguage maps file extension to language ID.
var extToLanguage = map[string]string{
	".go":   "go",
	".ts":   "typescript",
	".tsx":  "typescript",
	".js":   "javascript",
	".jsx":  "javascript",
	".py":   "python",
	".rs":   "rust",
	".c":    "c",
	".h":    "c",
	".cpp":  "cpp",
	".cc":   "cpp",
	".cxx":  "cpp",
	".hpp":  "cpp",
	".java": "java",
	".lua":  "lua",
	".zig":  "zig",
}

// Manager manages language server connections, one per language.
type Manager struct {
	cwd       string // immutable after construction
	mu        sync.Mutex
	clients   map[string]*Client // keyed by language ID
	starting  map[string]bool    // languages currently being started
	openFiles map[string]bool    // files already sent via didOpen
}

// NewManager creates a new server manager rooted at the given directory.
func NewManager(cwd string) *Manager {
	return &Manager{
		cwd:       cwd,
		clients:   make(map[string]*Client),
		starting:  make(map[string]bool),
		openFiles: make(map[string]bool),
	}
}

// CWD returns the root working directory (immutable).
func (m *Manager) CWD() string { return m.cwd }

// ForFile returns an LSP client for the given file, starting the server if needed.
func (m *Manager) ForFile(ctx context.Context, file string) (*Client, string, error) {
	lang := LanguageForFile(file)
	if lang == "" {
		return nil, "", fmt.Errorf("unsupported file type: %s", filepath.Ext(file))
	}

	// Fast path: server already running.
	m.mu.Lock()
	if c, ok := m.clients[lang]; ok {
		m.mu.Unlock()
		return c, lang, nil
	}

	// Wait for a server that's already starting.
	if m.starting[lang] {
		for range 3 {
			m.mu.Unlock()
			time.Sleep(200 * time.Millisecond)
			m.mu.Lock()
			if client, ok := m.clients[lang]; ok {
				m.mu.Unlock()
				return client, lang, nil
			}
			if !m.starting[lang] {
				break // startup failed, fall through to start new
			}
		}
		if m.starting[lang] {
			m.mu.Unlock()
			return nil, lang, fmt.Errorf("%s language server still starting after retries", lang)
		}
	}
	m.starting[lang] = true
	m.mu.Unlock()

	// Start server outside lock to avoid blocking other languages.
	c, err := m.startServer(ctx, lang)

	m.mu.Lock()
	delete(m.starting, lang)
	if err == nil {
		m.clients[lang] = c
	}
	m.mu.Unlock()

	if err != nil {
		return nil, lang, err
	}
	return c, lang, nil
}

func (m *Manager) startServer(ctx context.Context, lang string) (*Client, error) {
	configs, ok := defaultServers[lang]
	if !ok {
		return nil, fmt.Errorf("no language server configured for %s", lang)
	}

	for _, cfg := range configs {
		if _, err := exec.LookPath(cfg.Command); err != nil {
			continue
		}

		c, err := NewClient(ctx, cfg.Command, cfg.Args...)
		if err != nil {
			continue
		}

		if err := c.Initialize(ctx, m.cwd); err != nil {
			_ = c.Shutdown(ctx)
			continue
		}

		return c, nil
	}

	return nil, fmt.Errorf("no %s language server found in PATH (tried: %s)",
		lang, serverNames(configs))
}

// EnsureFileOpen sends didOpen for the file if not already open.
func (m *Manager) EnsureFileOpen(ctx context.Context, client *Client, file, lang string) error {
	m.mu.Lock()
	if m.openFiles[file] {
		m.mu.Unlock()
		return nil
	}
	m.openFiles[file] = true
	m.mu.Unlock()

	content, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	return client.DidOpen(ctx, file, lang, string(content))
}

// Shutdown stops all running language servers.
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for lang, c := range m.clients {
		_ = c.Shutdown(ctx)
		delete(m.clients, lang)
	}
}

// LanguageForFile returns the language ID for the given file path.
func LanguageForFile(file string) string {
	ext := strings.ToLower(filepath.Ext(file))
	return extToLanguage[ext]
}

// FindSymbolColumn finds the column (0-based) of a symbol on a given line.
func FindSymbolColumn(file string, line int, symbol string) (int, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return 0, fmt.Errorf("read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	if line < 0 || line >= len(lines) {
		return 0, fmt.Errorf("line %d out of range (file has %d lines)", line+1, len(lines))
	}

	lineText := lines[line]
	before, _, ok := strings.Cut(lineText, symbol)
	if !ok {
		return 0, fmt.Errorf("symbol %q not found on line %d", symbol, line+1)
	}

	// Convert byte offset to rune count (correct for BMP characters).
	col := len([]rune(before))
	return col, nil
}

func serverNames(configs []ServerConfig) string {
	names := make([]string, len(configs))
	for i, c := range configs {
		names[i] = c.Command
	}
	return strings.Join(names, ", ")
}
