package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// ---------------------------------------------------------------------------
// Protocol helpers
// ---------------------------------------------------------------------------

// isSymbolInformationArray checks whether raw JSON looks like SymbolInformation[]
// (has a "location" key) rather than DocumentSymbol[] (has "selectionRange").
// Both unmarshal without error in Go's lenient JSON decoder, so we probe the raw bytes.
func isSymbolInformationArray(raw json.RawMessage) bool {
	if len(raw) == 0 || raw[0] != '[' {
		return false
	}
	// Find the first object in the array and check for the "location" key.
	return strings.Contains(string(raw), `"location"`)
}

// ---------------------------------------------------------------------------
// URI helpers
// ---------------------------------------------------------------------------

func pathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if runtime.GOOS == "windows" {
		abs = "/" + filepath.ToSlash(abs)
	}
	u := &url.URL{Scheme: "file", Path: abs}
	return u.String()
}

// URIToPath converts a file:// URI to a filesystem path.
func URIToPath(uri string) string {
	return uriToPath(uri)
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return strings.TrimPrefix(uri, "file://")
	}
	path := u.Path
	if runtime.GOOS == "windows" {
		path = strings.TrimPrefix(path, "/")
		path = filepath.FromSlash(path)
	}
	return path
}
