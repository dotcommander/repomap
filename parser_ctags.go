package repomap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ctagsOnce      sync.Once
	ctagsAvailable bool
)

// CtagsAvailable reports whether ctags with JSON output support is on PATH.
func CtagsAvailable() bool {
	ctagsOnce.Do(func() {
		path, err := exec.LookPath("ctags")
		if err != nil {
			return
		}
		// Verify JSON output is supported (Universal Ctags, not Exuberant).
		out, err := exec.Command(path, "--output-format=json", "--version").Output()
		if err == nil && len(out) > 0 {
			ctagsAvailable = true
		}
	})
	return ctagsAvailable
}

// ctagsEntry is a single JSON line emitted by ctags --output-format=json.
type ctagsEntry struct {
	Type      string `json:"_type"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Scope     string `json:"scope"`
	ScopeKind string `json:"scopeKind"`
	Signature string `json:"signature"`
	Language  string `json:"language"`
	Line      int    `json:"line"`
}

// ParseWithCtags runs ctags once over all files and returns FileSymbols for
// each non-Go file in the list. Files must be absolute paths; root is used
// only for computing relative paths in output.
func ParseWithCtags(ctx context.Context, root string, files []FileInfo) ([]*FileSymbols, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Build the file list for ctags stdin (-L -).
	// We only pass non-Go files; Go files use the AST parser.
	absFiles := make([]string, 0, len(files))
	for _, f := range files {
		if f.Language != "go" {
			absFiles = append(absFiles, filepath.Join(root, f.Path))
		}
	}
	if len(absFiles) == 0 {
		return nil, nil
	}

	input := strings.Join(absFiles, "\n")

	cmd := exec.CommandContext(ctx, "ctags",
		"--output-format=json",
		"--fields=+S+l+K+Z+a+n",
		"-L", "-",
	)
	cmd.Stdin = strings.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ctags: %w: %s", err, stderr.String())
	}

	// Index FileInfo by absolute path for fast lookup.
	infoByAbs := make(map[string]FileInfo, len(files))
	for _, f := range files {
		infoByAbs[filepath.Join(root, f.Path)] = f
	}

	// symbols and members indexed by absolute path.
	type pendingMember struct {
		parentName string
		memberName string
	}
	symbolsByPath := make(map[string][]*Symbol)
	membersByPath := make(map[string][]pendingMember)
	// track symbol pointer by name+path for later field injection
	symIndex := make(map[string]*Symbol) // key: absPath+"\x00"+name

	dec := json.NewDecoder(&stdout)
	for dec.More() {
		var e ctagsEntry
		if err := dec.Decode(&e); err != nil {
			continue
		}
		if e.Type != "tag" || e.Name == "" {
			continue
		}

		absPath := e.Path
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(root, absPath)
		}

		info, ok := infoByAbs[absPath]
		if !ok {
			continue
		}

		// Collect member fields to attach to parent struct/class later.
		if isMemberKind(e.Kind) {
			if e.Scope != "" && isScopeContainer(e.ScopeKind) {
				membersByPath[absPath] = append(membersByPath[absPath], pendingMember{
					parentName: e.Scope,
					memberName: e.Name,
				})
			}
			continue
		}

		kind := mapCtagsKind(e.Kind)
		if kind == "" {
			continue
		}

		sym := &Symbol{
			Name:      e.Name,
			Kind:      kind,
			Signature: e.Signature,
			Exported:  isExportedForLang(e.Name, info.Language),
			Line:      e.Line,
		}

		symbolsByPath[absPath] = append(symbolsByPath[absPath], sym)
		symIndex[absPath+"\x00"+e.Name] = sym
	}

	// Attach collected member names to parent symbols.
	for absPath, members := range membersByPath {
		// Group members by parent name.
		grouped := make(map[string][]string)
		for _, m := range members {
			grouped[m.parentName] = append(grouped[m.parentName], m.memberName)
		}
		for parentName, fields := range grouped {
			if sym, ok := symIndex[absPath+"\x00"+parentName]; ok {
				sym.Signature = "{" + strings.Join(fields, ", ") + "}"
			}
		}
	}

	// Build output in input order.
	result := make([]*FileSymbols, 0, len(files))
	for _, f := range files {
		if f.Language == "go" {
			continue
		}
		absPath := filepath.Join(root, f.Path)
		syms := symbolsByPath[absPath]
		out := make([]Symbol, 0, len(syms))
		for _, s := range syms {
			out = append(out, *s)
		}
		result = append(result, &FileSymbols{
			Path:        f.Path,
			Language:    f.Language,
			Symbols:     out,
			ParseMethod: "ctags",
		})
	}
	return result, nil
}

// mapCtagsKind translates a ctags kind string to a Symbol Kind.
// Returns "" for kinds that should be skipped entirely.
func mapCtagsKind(k string) string {
	switch k {
	case "function", "f":
		return "function"
	case "method", "m":
		return "method"
	case "class", "c":
		return "class"
	case "struct", "s":
		return "struct"
	case "interface", "i":
		return "interface"
	case "enum", "e", "enumerator", "g":
		return "enum"
	case "variable", "v", "var":
		return "variable"
	case "constant", "d":
		return "constant"
	case "type", "t", "typedef":
		return "type"
	// module, namespace, package, member, field — skip
	default:
		return ""
	}
}

// isMemberKind returns true for ctags kinds that represent struct/class fields.
func isMemberKind(k string) bool {
	return k == "member" || k == "field"
}

// isScopeContainer returns true when the scope kind can own fields.
func isScopeContainer(k string) bool {
	return k == "struct" || k == "class"
}

// isExportedForLang applies language-specific export conventions.
func isExportedForLang(name, lang string) bool {
	if name == "" {
		return false
	}
	switch lang {
	case "go":
		return isExported(name)
	case "python":
		return !strings.HasPrefix(name, "_")
	default:
		return true
	}
}
