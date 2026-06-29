package repomap

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLanguageDocsMatchDeclaredSupport(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("docs/08-languages.md")
	require.NoError(t, err)
	doc := string(data)

	for _, def := range languageDefs {
		if def.ID == "go" {
			assert.Contains(t, doc, "Parsed with `go/ast` directly")
			continue
		}
		name := languageDocName(def.ID)
		switch def.Capability {
		case "full":
			assert.Contains(t, doc, "- "+name, "full language %q must be listed in docs/08-languages.md", def.ID)
		case "extension-only":
			assert.Contains(t, doc, "- "+name, "extension-only language %q must be listed in docs/08-languages.md", def.ID)
		default:
			t.Fatalf("unknown capability %q for %s", def.Capability, def.ID)
		}
	}

	assert.NotContains(t, treeSitterSection(doc), "CSS", "CSS is not declared in language.go")
}

func languageDocName(id string) string {
	switch id {
	case "typescript":
		return "TypeScript"
	case "tsx":
		return "TSX"
	case "javascript":
		return "JavaScript"
	case "jsx":
		return "JSX"
	case "python":
		return "Python"
	case "rust":
		return "Rust"
	case "c":
		return "C"
	case "cpp":
		return "C++"
	case "java":
		return "Java"
	case "ruby":
		return "Ruby"
	case "php":
		return "PHP"
	case "lua":
		return "Lua"
	case "zig":
		return "Zig"
	case "swift":
		return "Swift"
	case "kotlin":
		return "Kotlin"
	default:
		return id
	}
}

func treeSitterSection(doc string) string {
	_, after, ok := strings.Cut(doc, "## Tree-sitter supported")
	if !ok {
		return ""
	}
	section, _, _ := strings.Cut(after, "## ctags fallback")
	return section
}
