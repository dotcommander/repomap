package repomap

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxFileSize = 50_000

// bundleHashRe matches a hex hash segment (8+ chars) separated by dots or hyphens,
// as produced by webpack and other bundlers (e.g. main.a1b2c3d4.js, 4044.62596fd0.chunk.js).
var bundleHashRe = regexp.MustCompile(`[.\-][0-9a-f]{8,}[.\-]`)

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

// inSkipDir reports whether path has any component in skipDirs.
func inSkipDir(path string) bool {
	for _, part := range strings.Split(filepath.Dir(path), string(filepath.Separator)) {
		if skipDirs[part] {
			return true
		}
	}
	return false
}

// isBuildArtifact returns true for files that are likely minified, bundled, or generated output.
func isBuildArtifact(path string) bool {
	base := filepath.Base(path) //nolint:filepathbase // path is already a relative filename
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

// tooBig reports whether the file exceeds the maxFileSize limit.
func tooBig(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return info.Size() > maxFileSize
}
