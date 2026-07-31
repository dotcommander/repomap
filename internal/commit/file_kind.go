package commit

import (
	"path/filepath"
	"strings"
)

var codeExts = map[string]bool{
	".c": true, ".cc": true, ".cpp": true, ".go": true, ".h": true,
	".java": true, ".js": true, ".jsx": true, ".php": true, ".py": true,
	".rb": true, ".rs": true, ".ts": true, ".tsx": true,
}

var docExts = map[string]bool{
	".adoc": true, ".md": true, ".mdx": true, ".rst": true, ".txt": true,
}

func isTestFile(path string) bool {
	ext := filepath.Ext(path)
	base := filepath.Base(path)
	switch ext {
	case ".go":
		return strings.HasSuffix(path, "_test.go")
	case ".py":
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
	case ".rs":
		return strings.Contains(path, "/tests/") || strings.HasSuffix(base, "_test.rs")
	case ".java":
		return strings.HasSuffix(base, "Test.java") || strings.HasSuffix(base, "Tests.java")
	case ".ts", ".tsx", ".js", ".jsx":
		return strings.HasSuffix(base, ".test"+ext) || strings.HasSuffix(base, ".spec"+ext)
	case ".rb":
		return strings.HasSuffix(base, "_test.rb") || strings.HasSuffix(base, "_spec.rb")
	case ".php":
		return strings.HasSuffix(base, "Test.php")
	default:
		return false
	}
}

func pluginSegment(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) > 1 && parts[0] == "plugins" {
		return parts[1]
	}
	return ""
}
