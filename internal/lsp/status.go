package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const defaultStatusMaxDepth = 3

var lookPath = exec.LookPath

// StatusReport describes LSP coverage detected from source files and project
// markers without starting any language servers.
type StatusReport struct {
	Root    string          `json:"root"`
	Servers []StatusServer  `json:"servers"`
	Missing []MissingServer `json:"missing"`
}

// StatusServer is one detected language server root with an available command.
type StatusServer struct {
	Language  string   `json:"language"`
	Root      string   `json:"root"`
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	FileTypes []string `json:"file_types"`
}

// MissingServer reports source files for a language whose configured LSP
// command is not available on PATH.
type MissingServer struct {
	Language        string   `json:"language"`
	TriedCommands   []string `json:"tried_commands"`
	FoundExtensions []string `json:"found_extensions"`
}

type statusServerConfig struct {
	Language    string
	FileTypes   []string
	RootMarkers []string
	Configs     []ServerConfig
}

var statusServerConfigs = []statusServerConfig{
	{Language: "go", FileTypes: []string{".go"}, RootMarkers: []string{"go.mod", "go.work"}, Configs: defaultServers["go"]},
	{Language: "typescript", FileTypes: []string{".ts", ".tsx"}, RootMarkers: []string{"tsconfig.json", "package.json"}, Configs: defaultServers["typescript"]},
	{Language: "javascript", FileTypes: []string{".js", ".jsx"}, RootMarkers: []string{"package.json"}, Configs: defaultServers["javascript"]},
	{Language: "python", FileTypes: []string{".py"}, RootMarkers: []string{"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt"}, Configs: defaultServers["python"]},
	{Language: "rust", FileTypes: []string{".rs"}, RootMarkers: []string{"Cargo.toml"}, Configs: defaultServers["rust"]},
	{Language: "c", FileTypes: []string{".c", ".h"}, RootMarkers: []string{"compile_commands.json", "CMakeLists.txt", "Makefile"}, Configs: defaultServers["c"]},
	{Language: "cpp", FileTypes: []string{".cpp", ".cc", ".cxx", ".hpp"}, RootMarkers: []string{"compile_commands.json", "CMakeLists.txt", "Makefile"}, Configs: defaultServers["cpp"]},
	{Language: "java", FileTypes: []string{".java"}, RootMarkers: []string{"pom.xml", "build.gradle", "build.gradle.kts"}, Configs: defaultServers["java"]},
	{Language: "lua", FileTypes: []string{".lua"}, RootMarkers: []string{".luarc.json", ".luarc.jsonc"}, Configs: defaultServers["lua"]},
	{Language: "zig", FileTypes: []string{".zig"}, RootMarkers: []string{"build.zig"}, Configs: defaultServers["zig"]},
}

// DetectStatus scans root for source files and project markers, then reports
// which configured language servers are available. It does not start servers.
func DetectStatus(ctx context.Context, root string) (StatusReport, error) {
	return detectStatus(ctx, root, defaultStatusMaxDepth)
}

func detectStatus(ctx context.Context, root string, maxDepth int) (StatusReport, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return StatusReport{}, err
	}

	hints, err := collectStatusHints(ctx, absRoot, maxDepth)
	if err != nil {
		return StatusReport{}, err
	}

	report := StatusReport{
		Root:    absRoot,
		Servers: []StatusServer{},
		Missing: []MissingServer{},
	}
	for _, cfg := range statusServerConfigs {
		available, command := firstAvailableServer(cfg.Configs)
		roots := rootsForConfig(cfg, hints, absRoot)
		if len(roots) == 0 {
			continue
		}
		if available {
			for _, root := range roots {
				report.Servers = append(report.Servers, StatusServer{
					Language:  cfg.Language,
					Root:      root,
					Command:   command.Command,
					Args:      append([]string(nil), command.Args...),
					FileTypes: append([]string(nil), cfg.FileTypes...),
				})
			}
			continue
		}
		report.Missing = append(report.Missing, MissingServer{
			Language:        cfg.Language,
			TriedCommands:   serverNamesSlice(cfg.Configs),
			FoundExtensions: foundExtensions(cfg.FileTypes, hints.foundExtensions),
		})
	}

	slices.SortFunc(report.Servers, func(a, b StatusServer) int {
		if c := strings.Compare(a.Root, b.Root); c != 0 {
			return c
		}
		return strings.Compare(a.Language, b.Language)
	})
	slices.SortFunc(report.Missing, func(a, b MissingServer) int {
		return strings.Compare(a.Language, b.Language)
	})
	return report, nil
}

type statusHints struct {
	markerMatches   map[string][]string
	foundExtensions map[string]bool
}

func collectStatusHints(ctx context.Context, root string, maxDepth int) (statusHints, error) {
	hints := statusHints{
		markerMatches:   make(map[string][]string),
		foundExtensions: make(map[string]bool),
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !entry.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext != "" {
				hints.foundExtensions[ext] = true
			}
			return nil
		}
		if path != root && shouldSkipStatusDir(entry.Name()) {
			return filepath.SkipDir
		}
		if path != root && depthFromRoot(root, path) > maxDepth {
			return filepath.SkipDir
		}
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return nil
		}
		names := make(map[string]bool, len(entries))
		for _, ent := range entries {
			names[ent.Name()] = true
		}
		for _, cfg := range statusServerConfigs {
			for _, marker := range cfg.RootMarkers {
				if names[marker] {
					hints.markerMatches[cfg.Language] = append(hints.markerMatches[cfg.Language], path)
					break
				}
			}
		}
		return nil
	})
	return hints, err
}

func shouldSkipStatusDir(name string) bool {
	switch name {
	case ".git", "vendor", "node_modules", "__pycache__", ".venv", "build", "dist", "target", "out", ".work", ".tmp", "tmp", ".cache":
		return true
	default:
		return false
	}
}

func depthFromRoot(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

func rootsForConfig(cfg statusServerConfig, hints statusHints, root string) []string {
	if !hasAnyExtension(cfg.FileTypes, hints.foundExtensions) {
		return nil
	}
	roots := append([]string(nil), hints.markerMatches[cfg.Language]...)
	if len(roots) == 0 {
		roots = append(roots, root)
	}
	return dedupeTopmostRoots(roots)
}

func hasAnyExtension(fileTypes []string, found map[string]bool) bool {
	for _, ext := range fileTypes {
		if found[ext] {
			return true
		}
	}
	return false
}

func foundExtensions(fileTypes []string, found map[string]bool) []string {
	var out []string
	for _, ext := range fileTypes {
		if found[ext] {
			out = append(out, ext)
		}
	}
	return out
}

func dedupeTopmostRoots(roots []string) []string {
	slices.Sort(roots)
	var out []string
	for _, root := range roots {
		root = filepath.Clean(root)
		if len(out) > 0 && isPathWithin(root, out[len(out)-1]) {
			continue
		}
		out = append(out, root)
	}
	return out
}

func isPathWithin(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func firstAvailableServer(configs []ServerConfig) (bool, ServerConfig) {
	for _, cfg := range configs {
		if _, err := lookPath(cfg.Command); err == nil {
			return true, cfg
		}
	}
	return false, ServerConfig{}
}

func serverNamesSlice(configs []ServerConfig) []string {
	names := make([]string, 0, len(configs))
	for _, cfg := range configs {
		names = append(names, cfg.Command)
	}
	return names
}
