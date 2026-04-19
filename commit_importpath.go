package repomap

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Edge weights used by commit_grouping.buildEdges. Grouped here so the
// full edge-weight vocabulary is visible in one place and the clustering
// contract (DefaultConfidenceCutoff) has something to derive from.
//
// Cluster-forming edges (weight >= DefaultConfidenceCutoff):
//   - WeightTestPair — a test and its implementation file
//   - WeightSymbolDep — one file imports another (exact-syntax match;
//     Go module path, PHP FQCN `use`, Python dotted package, Rust `use`
//     path, TS relative `import`)
//
// Refine-only edges (below the cutoff; tie-break within a cluster but
// never form one on their own):
//   - WeightCoChange — historical co-commit frequency
//   - WeightSibling — same directory + same inferred type
const (
	WeightTestPair  = 1.0
	WeightSymbolDep = 0.8
	WeightCoChange  = 0.5
	WeightSibling   = 0.3
)

var phpNamespaceRe = regexp.MustCompile(`(?i)^namespace\s+([\w\\]+)`)
var javaPackageRe = regexp.MustCompile(`^package\s+([\w.]+)\s*;`)

// deriveImportPath returns the best-available ImportPath for a non-Go file.
// Returns "" when no reliable identity can be determined. The result is
// intentionally coarse — it identifies the logical package/module/namespace,
// not a globally-unique path, because commit grouping only needs to match
// files within the same repo.
func deriveImportPath(absPath, root, lang string, lines []string) string {
	switch lang {
	case "php":
		return derivePHPNamespace(lines)
	case "python":
		return derivePythonPackage(absPath, root)
	case "java":
		return deriveJavaPackage(lines)
	case "rust":
		return deriveRustCratePath(absPath, root)
	case "typescript", "tsx", "javascript", "jsx":
		return deriveTSPackagePath(absPath, root)
	}
	return ""
}

// derivePHPNamespace reads the first `namespace` declaration in the file.
// PHP namespaces use backslash separators (e.g. App\Http\Controllers).
func derivePHPNamespace(lines []string) string {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if m := phpNamespaceRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// derivePythonPackage walks upward from absPath looking for __init__.py files.
// The package identity is the dotted path from the highest ancestor directory
// that still contains __init__.py. Returns "" for script files with no package.
func derivePythonPackage(absPath, root string) string {
	dir := filepath.Dir(absPath)
	if !strings.HasPrefix(dir, root) {
		return ""
	}
	// Walk up collecting directories that have __init__.py.
	var parts []string
	for {
		init := filepath.Join(dir, "__init__.py")
		if _, err := os.Stat(init); err != nil {
			break // no __init__.py here — stop
		}
		parts = append([]string{filepath.Base(dir)}, parts...)
		parent := filepath.Dir(dir)
		if parent == dir || !strings.HasPrefix(parent, root) {
			break
		}
		dir = parent
	}
	if len(parts) == 0 {
		return "" // script file
	}
	return strings.Join(parts, ".")
}

// deriveJavaPackage reads the `package` declaration from Java source lines.
func deriveJavaPackage(lines []string) string {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if m := javaPackageRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// deriveRustCratePath derives a crate-relative path from a Cargo.toml lookup.
// Format: "<crate-name>::<mod-path>" where mod-path uses "::" separators
// mirroring the Rust module system. Returns "" when no Cargo.toml is found.
func deriveRustCratePath(absPath, root string) string {
	crateName, crateRoot := findCargoToml(absPath, root)
	if crateName == "" {
		return ""
	}
	rel, err := filepath.Rel(crateRoot, absPath)
	if err != nil {
		return ""
	}
	// Strip src/ prefix (conventional Rust layout).
	rel = strings.TrimPrefix(rel, "src"+string(filepath.Separator))
	// Drop .rs extension and convert path separators to ::.
	rel = strings.TrimSuffix(rel, ".rs")
	rel = strings.ReplaceAll(rel, string(filepath.Separator), "::")
	// mod.rs is the directory's module — represent as the parent name.
	if strings.HasSuffix(rel, "::mod") {
		rel = strings.TrimSuffix(rel, "::mod")
	}
	if rel == "mod" || rel == "" {
		rel = "lib"
	}
	return crateName + "::" + rel
}

// findCargoToml walks upward from dir looking for a Cargo.toml file.
// Returns the crate name and the directory containing Cargo.toml, or ("","").
func findCargoToml(absPath, root string) (string, string) {
	dir := filepath.Dir(absPath)
	for {
		if !strings.HasPrefix(dir, root) {
			return "", ""
		}
		cargo := filepath.Join(dir, "Cargo.toml")
		data, err := os.ReadFile(cargo)
		if err == nil {
			name := parseCargoName(string(data))
			if name != "" {
				return name, dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", ""
}

// parseCargoName extracts the `name = "..."` field from Cargo.toml content.
var cargoNameRe = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)

func parseCargoName(content string) string {
	if m := cargoNameRe.FindStringSubmatch(content); m != nil {
		return m[1]
	}
	return ""
}

// deriveTSPackagePath derives a package-relative import identity from the
// nearest package.json. Format: "<package-name>/<rel-path-without-ext>".
// Returns "" when no package.json is found.
func deriveTSPackagePath(absPath, root string) string {
	pkgName, pkgRoot := findPackageJSON(absPath, root)
	if pkgName == "" {
		return ""
	}
	rel, err := filepath.Rel(pkgRoot, absPath)
	if err != nil {
		return ""
	}
	// Normalize: drop extension, convert OS separators to /.
	ext := filepath.Ext(rel)
	rel = strings.TrimSuffix(rel, ext)
	rel = filepath.ToSlash(rel)
	// index files represent the directory itself.
	if strings.HasSuffix(rel, "/index") {
		rel = strings.TrimSuffix(rel, "/index")
	}
	if rel == "" || rel == "index" {
		return pkgName
	}
	return pkgName + "/" + rel
}

// findPackageJSON walks upward from dir looking for a package.json file.
// Returns the package name and the directory containing it, or ("","").
func findPackageJSON(absPath, root string) (string, string) {
	dir := filepath.Dir(absPath)
	for {
		if !strings.HasPrefix(dir, root) {
			return "", ""
		}
		pkg := filepath.Join(dir, "package.json")
		data, err := os.ReadFile(pkg)
		if err == nil {
			name := parsePackageJSONName(string(data))
			if name != "" {
				return name, dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", ""
}

// parsePackageJSONName extracts the `"name"` field from package.json content
// using a simple regex — avoids pulling in a JSON library for this hot path.
var pkgJSONNameRe = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)

func parsePackageJSONName(content string) string {
	if m := pkgJSONNameRe.FindStringSubmatch(content); m != nil {
		return m[1]
	}
	return ""
}
