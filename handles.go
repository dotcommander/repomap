package repomap

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	fileHandlePrefix   = "file:"
	symbolHandlePrefix = "symbol:"
)

// FileHandle returns a stable handle for a repository-relative file path.
func FileHandle(path string) string {
	if path == "" {
		return ""
	}
	return fileHandlePrefix + path
}

// SymbolHandle returns a stable handle for a symbol in a repository-relative file.
func SymbolHandle(file string, sym Symbol) string {
	if file == "" || sym.Name == "" || sym.Line <= 0 {
		return ""
	}
	return fmt.Sprintf("%s%s::%s#%s@%d", symbolHandlePrefix, file, sym.Name, sym.Kind, sym.Line)
}

// ParseSymbolHandle parses handles emitted by SymbolHandle.
func ParseSymbolHandle(handle string) (file, name, kind string, line int, ok bool) {
	if !strings.HasPrefix(handle, symbolHandlePrefix) {
		return "", "", "", 0, false
	}
	body := strings.TrimPrefix(handle, symbolHandlePrefix)
	filePart, rest, found := strings.Cut(body, "::")
	if !found || filePart == "" {
		return "", "", "", 0, false
	}
	namePart, linePart, found := strings.Cut(rest, "@")
	if !found {
		return "", "", "", 0, false
	}
	name, kind, found = strings.Cut(namePart, "#")
	if !found || name == "" || kind == "" {
		return "", "", "", 0, false
	}
	parsedLine, err := strconv.Atoi(linePart)
	if err != nil {
		return "", "", "", 0, false
	}
	return filePart, name, kind, parsedLine, true
}
