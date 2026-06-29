//go:build !notreesitter

package repomap

import (
	"fmt"
	"sort"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func extractCallSites(source []byte, root *tree_sitter.Node, lang *tree_sitter.Language, langID string, fs *FileSymbols) {
	queryStr := tsCallSiteQuery(langID)
	if queryStr == "" {
		return
	}
	seen := make(map[string]struct{})
	runTSQuery(source, root, lang, queryStr, func(m *tree_sitter.QueryMatch, q *tree_sitter.Query) {
		n := tsCaptureNode(m, q, "call")
		if n == nil {
			return
		}
		name := normalizeCallSiteName(n.Utf8Text(source))
		if name == "" {
			return
		}
		line := int(n.StartPosition().Row + 1)
		key := fmt.Sprintf("%s:%d", name, line)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		fs.CallSites = append(fs.CallSites, CallSite{Name: name, Line: line})
	})
}

func normalizeCallSiteName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

func tsCallSiteQuery(langID string) string {
	switch langID {
	case "javascript", "typescript", "tsx", "jsx":
		return `
(call_expression
  function: (_) @call)

(new_expression
  constructor: (_) @call)
`
	case "python":
		return `
(call
  function: (identifier) @call)

(call
  function: (attribute
    attribute: (identifier) @call))
`
	case "rust":
		return `
(call_expression
  function: (identifier) @call)

(call_expression
  function: (field_expression
    field: (field_identifier) @call))

(macro_invocation
  macro: (identifier) @call)
`
	case "c":
		return `
(call_expression
  function: (identifier) @call)
`
	case "cpp":
		return `
(call_expression
  function: (identifier) @call)

(call_expression
  function: (field_expression
    field: (field_identifier) @call))
`
	case "java":
		return `
(method_invocation
  name: (identifier) @call)

(object_creation_expression
  type: (type_identifier) @call)
`
	case "ruby":
		return `
(call
  method: (identifier) @call)
`
	default:
		return ""
	}
}

const (
	callSiteBonusPerRef = 4
	callSiteMaxBonus    = 32
)

func ApplyCallSiteReferenceBonus(ranked []RankedFile) {
	if len(ranked) == 0 {
		return
	}

	byName := make(map[string][]string)
	for _, rf := range ranked {
		if rf.FileSymbols == nil || rf.Language == "go" {
			continue
		}
		for _, sym := range rf.Symbols {
			if !sym.Exported || !symbolRefNameOK(sym.Name) {
				continue
			}
			byName[sym.Name] = append(byName[sym.Name], rf.Path)
		}
	}
	if len(byName) == 0 {
		return
	}

	callersByTarget := make(map[string]map[string]struct{})
	for _, rf := range ranked {
		if rf.FileSymbols == nil || rf.Language == "go" {
			continue
		}
		seenInCaller := make(map[string]struct{})
		for _, site := range rf.CallSites {
			for _, name := range callSiteLookupNames(site.Name) {
				for _, targetPath := range byName[name] {
					if targetPath == rf.Path {
						continue
					}
					seenInCaller[targetPath] = struct{}{}
				}
			}
		}
		for targetPath := range seenInCaller {
			if callersByTarget[targetPath] == nil {
				callersByTarget[targetPath] = make(map[string]struct{})
			}
			callersByTarget[targetPath][rf.Path] = struct{}{}
		}
	}

	for i := range ranked {
		callers := callersByTarget[ranked[i].Path]
		if len(callers) == 0 {
			continue
		}
		bonus := len(callers) * callSiteBonusPerRef
		if bonus > callSiteMaxBonus {
			bonus = callSiteMaxBonus
		}
		addScoreComponent(&ranked[i], scoreComponentCallSites, bonus)
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Path < ranked[j].Path
	})
}

func callSiteLookupNames(name string) []string {
	name = normalizeCallSiteName(name)
	if name == "" {
		return nil
	}
	names := []string{name}
	if idx := strings.LastIndex(name, "."); idx >= 0 && idx < len(name)-1 {
		names = append(names, name[idx+1:])
	}
	if strings.HasSuffix(name, "()") {
		trimmed := strings.TrimSuffix(name, "()")
		names = append(names, trimmed)
		if idx := strings.LastIndex(trimmed, "."); idx >= 0 && idx < len(trimmed)-1 {
			names = append(names, trimmed[idx+1:])
		}
	}
	return names
}
