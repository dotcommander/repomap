package repomap

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sync/errgroup"
)

var errUnsafeRepositoryFile = errors.New("unsafe repository file")

// atomicWriteFile writes data to path via a uniquely named temp file + rename.
// Unique temp names keep concurrent writers to the same path from publishing
// each other's partial bytes (a fixed ".tmp" name let writer A rename writer
// B's half-written file into place); Sync bounds torn writes across crashes.
func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".atomic-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// readRepositoryRegularFile reads a regular file contained by repoRoot. It
// rejects absolute and escaping paths, directories, and symlinks so commit
// rewrite flows never follow a target outside the repository.
func readRepositoryRegularFile(repoRoot, path string) (string, []byte, error) {
	abs, _, err := repositoryRegularFile(repoRoot, path)
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", nil, err
	}
	return abs, data, nil
}

// rewriteRepositoryRegularFile atomically rewrites a regular repository file
// while preserving its existing permission bits. It validates the target again
// immediately before publishing the replacement.
func rewriteRepositoryRegularFile(repoRoot, path string, data []byte) error {
	abs, mode, err := repositoryRegularFile(repoRoot, path)
	if err != nil {
		return err
	}
	return atomicWriteFile(abs, data, mode.Perm())
}

func repositoryRegularFile(repoRoot, path string) (string, fs.FileMode, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", 0, fmt.Errorf("%w: invalid repository path %q", errUnsafeRepositoryFile, path)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", 0, fmt.Errorf("%w: repository path escapes root: %q", errUnsafeRepositoryFile, path)
	}

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", 0, fmt.Errorf("resolve repository root: %w", err)
	}

	current := root
	parts := strings.Split(clean, string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", 0, err
		}
		if i < len(parts)-1 {
			if !info.IsDir() {
				return "", 0, fmt.Errorf("%w: repository path contains a non-directory: %q", errUnsafeRepositoryFile, path)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return "", 0, fmt.Errorf("%w: repository path is not a regular file: %q", errUnsafeRepositoryFile, path)
		}
		return current, info.Mode(), nil
	}
	return "", 0, fmt.Errorf("%w: invalid repository path %q", errUnsafeRepositoryFile, path)
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
