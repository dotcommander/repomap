// Package safefs provides narrow filesystem primitives for cache publication
// and repository-contained regular-file rewrites.
package safefs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsafeRepositoryFile reports a path that cannot safely be read or
// rewritten as a regular file contained by a repository root.
var ErrUnsafeRepositoryFile = errors.New("unsafe repository file")

// ErrRepositoryFileTooLarge reports a repository file that exceeds a caller's
// bounded-read limit.
var ErrRepositoryFileTooLarge = errors.New("repository file exceeds read limit")

// AtomicWriteFile publishes data via a unique temporary file and rename.
// Unique names keep concurrent writers from publishing each other's partial
// data. Sync bounds torn writes across crashes.
func AtomicWriteFile(path string, data []byte, perm fs.FileMode) error {
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

// ReadRepositoryRegularFile reads a regular file contained by repoRoot. It
// rejects absolute and escaping paths, directories, and symlinks.
func ReadRepositoryRegularFile(repoRoot, path string) (string, []byte, error) {
	rootPath, clean, root, _, err := openRepositoryRegularFile(repoRoot, path)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = root.Close() }()

	data, err := root.ReadFile(clean)
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(rootPath, clean), data, nil
}

// ReadRepositoryRegularFileLimit reads at most maxBytes from a regular file
// contained by repoRoot. It reads one extra byte to detect overflow without
// allocating the full file.
func ReadRepositoryRegularFileLimit(repoRoot, path string, maxBytes int64) (string, []byte, error) {
	if maxBytes < 0 {
		return "", nil, fmt.Errorf("repository file read limit must not be negative")
	}
	rootPath, clean, root, _, err := openRepositoryRegularFile(repoRoot, path)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = root.Close() }()

	f, err := root.Open(clean)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("%w: repository path is not a regular file: %q", ErrUnsafeRepositoryFile, path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", nil, err
	}
	if int64(len(data)) > maxBytes {
		return "", nil, fmt.Errorf("%w: %q", ErrRepositoryFileTooLarge, path)
	}
	return filepath.Join(rootPath, clean), data, nil
}

// RewriteRepositoryRegularFile atomically rewrites a regular repository file
// while preserving its existing permission bits. It validates the target again
// immediately before publishing the replacement.
func RewriteRepositoryRegularFile(repoRoot, path string, data []byte) error {
	_, clean, root, mode, err := openRepositoryRegularFile(repoRoot, path)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	return atomicWriteRoot(root, clean, data, mode.Perm())
}

// RepositoryRegularFile validates and locates a regular file contained by
// repoRoot without following symlinks in any path component.
func RepositoryRegularFile(repoRoot, path string) (string, fs.FileMode, error) {
	return repositoryRegularFile(repoRoot, path)
}

func repositoryRegularFile(repoRoot, path string) (string, fs.FileMode, error) {
	rootPath, clean, root, mode, err := openRepositoryRegularFile(repoRoot, path)
	if err != nil {
		return "", 0, err
	}
	_ = root.Close()
	return filepath.Join(rootPath, clean), mode, nil
}

func openRepositoryRegularFile(repoRoot, path string) (string, string, *os.Root, fs.FileMode, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", "", nil, 0, fmt.Errorf("%w: invalid repository path %q", ErrUnsafeRepositoryFile, path)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", nil, 0, fmt.Errorf("%w: repository path escapes root: %q", ErrUnsafeRepositoryFile, path)
	}

	rootPath, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", "", nil, 0, fmt.Errorf("resolve repository root: %w", err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", "", nil, 0, fmt.Errorf("open repository root: %w", err)
	}

	parts := strings.Split(clean, string(filepath.Separator))
	for i := range parts {
		current := filepath.Join(parts[:i+1]...)
		info, err := root.Lstat(current)
		if err != nil {
			_ = root.Close()
			return "", "", nil, 0, err
		}
		if i < len(parts)-1 {
			if !info.IsDir() {
				_ = root.Close()
				return "", "", nil, 0, fmt.Errorf("%w: repository path contains a non-directory: %q", ErrUnsafeRepositoryFile, path)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			_ = root.Close()
			return "", "", nil, 0, fmt.Errorf("%w: repository path is not a regular file: %q", ErrUnsafeRepositoryFile, path)
		}
		return rootPath, clean, root, info.Mode(), nil
	}
	_ = root.Close()
	return "", "", nil, 0, fmt.Errorf("%w: invalid repository path %q", ErrUnsafeRepositoryFile, path)
}

func atomicWriteRoot(root *os.Root, path string, data []byte, perm fs.FileMode) error {
	f, tmp, err := createRootTemp(root, filepath.Dir(path))
	if err != nil {
		return err
	}
	cleanup := func() {
		_ = f.Close()
		_ = root.Remove(tmp)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		_ = root.Remove(tmp)
		return err
	}
	if err := root.Rename(tmp, path); err != nil {
		_ = root.Remove(tmp)
		return err
	}
	return nil
}

func createRootTemp(root *os.Root, dir string) (*os.File, string, error) {
	for range 100 {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return nil, "", err
		}
		name := filepath.Join(dir, ".atomic-"+hex.EncodeToString(suffix[:]))
		f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return f, name, err
	}
	return nil, "", fmt.Errorf("create repository temp file: too many name collisions")
}
