package repomap

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// supportedExts maps file extensions to language IDs.
var supportedExts = map[string]string{
	".go":    "go",
	".ts":    "typescript",
	".tsx":   "typescript",
	".js":    "javascript",
	".jsx":   "javascript",
	".py":    "python",
	".rs":    "rust",
	".c":     "c",
	".h":     "c",
	".cpp":   "cpp",
	".cc":    "cpp",
	".java":  "java",
	".lua":   "lua",
	".zig":   "zig",
	".rb":    "ruby",
	".swift": "swift",
	".kt":    "kotlin",
	".php":   "php",
}

const maxFileSize = 50_000

// skipDirs holds directory names to skip during filesystem walk.
var skipDirs = map[string]bool{
	// VCS
	".git": true,

	// Dependency caches
	"vendor":       true,
	"node_modules": true,
	"__pycache__":  true,
	".venv":        true,

	// Build output
	"build":       true,
	"dist":        true,
	"target":      true,
	"out":         true,
	"_app":        true, // SvelteKit build output
	".svelte-kit": true,
	".next":       true, // Next.js build output
	".nuxt":       true, // Nuxt build output
	".output":     true, // Nitro/Nuxt output
	"coverage":    true,

	// Scratch / temp
	".work": true,
	".tmp":  true,
	"tmp":   true,

	// Caches
	".cache":        true,
	".parcel-cache": true, // Parcel bundler
	".turbo":        true, // Turborepo cache
	".angular":      true, // Angular cache

	// IDE / tooling config
	".idea":         true, // JetBrains
	".vscode":       true, // VS Code
	".devcontainer": true, // Dev container config

	// Test fixtures
	"testdata": true, // Go test fixtures

	// Infrastructure tooling
	".terraform": true, // Terraform state

	// JVM build tools
	".gradle": true, // Gradle cache
	".mvn":    true, // Maven wrapper

	// Language-specific package managers
	".bundle":    true, // Ruby bundler
	"Pods":       true, // CocoaPods
	".dart_tool": true, // Dart toolchain
	".pub-cache": true, // Dart pub cache

	// Codegen output
	"__generated__":    true, // GraphQL / generic codegen
	"storybook-static": true, // Storybook build output
}

// FileInfo holds a discovered file with its language.
type FileInfo struct {
	Path     string // relative to project root
	Language string // language ID
}

// isInsideGitRepo reports whether dir is inside a git repository by walking
// up the directory tree looking for a .git entry.
func isInsideGitRepo(dir string) bool {
	for {
		_, err := os.Stat(filepath.Join(dir, ".git"))
		if err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// ScanFiles discovers source files in the given directory.
// Uses git ls-files when available, falls back to directory walking.
func ScanFiles(ctx context.Context, root string) ([]FileInfo, error) {
	if !isInsideGitRepo(root) {
		return nil, nil
	}

	files, err := scanGit(ctx, root)
	if err != nil {
		files, err = scanWalk(ctx, root)
		if err != nil {
			return nil, err
		}
	}

	slices.SortFunc(files, func(a, b FileInfo) int {
		return strings.Compare(a.Path, b.Path)
	})

	return files, nil
}

// LanguageFor returns the language ID for a file extension, or "" if unsupported.
func LanguageFor(ext string) string {
	return supportedExts[ext]
}

func scanGit(ctx context.Context, root string) ([]FileInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if inSkipDir(line) {
			continue
		}

		lang := LanguageFor(filepath.Ext(line))
		if lang == "" {
			continue
		}

		absPath := filepath.Join(root, line)
		if tooBig(absPath) || isBuildArtifact(line) {
			continue
		}

		files = append(files, FileInfo{Path: line, Language: lang})
	}

	return files, nil
}

func scanWalk(ctx context.Context, root string) ([]FileInfo, error) {
	var files []FileInfo

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			// Skip nested git repos and submodules
			if path != root {
				if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}

		lang := LanguageFor(filepath.Ext(path))
		if lang == "" {
			return nil
		}

		if tooBig(path) || isBuildArtifact(path) {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil //nolint:nilerr // skip if relative path can't be computed
		}

		files = append(files, FileInfo{Path: rel, Language: lang})
		return nil
	})

	return files, err
}

// inSkipDir reports whether path has any component in skipDirs.
func inSkipDir(path string) bool {
	for _, part := range strings.Split(filepath.Dir(path), string(filepath.Separator)) {
		if skipDirs[part] {
			return true
		}
	}
	return false
}

func tooBig(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return info.Size() > maxFileSize
}

// bundleHashRe matches a hex hash segment (8+ chars) separated by dots or hyphens,
// as produced by webpack and other bundlers (e.g. main.a1b2c3d4.js, 4044.62596fd0.chunk.js).
var bundleHashRe = regexp.MustCompile(`[.\-][0-9a-f]{8,}[.\-]`)

// isBuildArtifact returns true for files that are likely minified, bundled, or generated output.
func isBuildArtifact(path string) bool {
	base := filepath.Base(path)
	switch {
	// Minified assets
	case strings.HasSuffix(base, ".min.js"),
		strings.HasSuffix(base, ".min.css"):
		return true
	// Bundler output
	case strings.HasSuffix(base, ".bundle.js"),
		strings.HasSuffix(base, ".chunk.js"),
		strings.HasSuffix(base, ".chunk.css"):
		return true
	// Compiled output (e.g. Wasm glue)
	case strings.HasSuffix(base, "_compiled.js"):
		return true
	// Codegen output
	case strings.Contains(base, ".generated."):
		return true
	// Webpack / bundler content-hash filenames
	case bundleHashRe.MatchString(base):
		return true
	}
	return false
}
