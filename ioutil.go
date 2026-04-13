package repomap

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sync/errgroup"
)

// atomicWriteFile writes data to path via temp file + rename (prevents partial writes).
func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// collectNonNil filters nil pointers from a slice.
func collectNonNil[T any](slice []*T) []*T {
	var result []*T
	for _, v := range slice {
		if v != nil {
			result = append(result, v)
		}
	}
	return result
}

// relPath returns the relative path from root to path, falling back to path on error.
func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

// parallelParse runs fn on each item in parallel using an errgroup bounded
// to NumCPU goroutines. Returns non-nil results in input order.
func parallelParse[T any](items []T, fn func(T) *FileSymbols) []*FileSymbols {
	results := make([]*FileSymbols, len(items))
	g := new(errgroup.Group)
	g.SetLimit(runtime.NumCPU())
	for i, item := range items {
		g.Go(func() error {
			results[i] = fn(item)
			return nil
		})
	}
	_ = g.Wait()
	return collectNonNil(results)
}

// longestCommonPrefix returns the longest common prefix of a sorted slice of strings.
// The prefix is trimmed to an identifier boundary (underscore or camelCase).
// Operates on runes to avoid splitting multi-byte UTF-8 sequences.
func longestCommonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	prefix := []rune(names[0])
	for _, name := range names[1:] {
		nameR := []rune(name)
		// Trim prefix to the common run with nameR.
		n := len(prefix)
		if len(nameR) < n {
			n = len(nameR)
		}
		i := 0
		for i < n && prefix[i] == nameR[i] {
			i++
		}
		prefix = prefix[:i]
		if len(prefix) == 0 {
			return ""
		}
	}
	return trimIdentifierPrefix(string(prefix))
}

func trimIdentifierPrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	runes := []rune(prefix)
	lastBoundary := -1
	for i := 1; i < len(runes); i++ {
		if runes[i] == '_' || isCamelBoundary(runes[i-1], runes[i]) {
			lastBoundary = i
		}
	}
	if lastBoundary > 0 {
		return string(runes[:lastBoundary])
	}
	return prefix
}

func isCamelBoundary(prev, curr rune) bool {
	return prev >= 'a' && prev <= 'z' && curr >= 'A' && curr <= 'Z'
}
