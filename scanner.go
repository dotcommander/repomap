package repomap

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// FileInfo holds a discovered file with its language.
type FileInfo struct {
	Path     string // relative to project root
	Language string // language ID
}

// isInsideGitRepo reports whether dir is inside a git repository by walking
// up the directory tree looking for a .git entry.
func isInsideGitRepo(dir string) bool {
	for {
		_, err := os.Stat(filepath.Join(dir, ".git"))
		if err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// ScanFiles discovers source files in the given directory.
// Falls back to directory walk if not inside a git repo or if git ls-files fails.
func ScanFiles(ctx context.Context, root string) ([]FileInfo, error) {
	var files []FileInfo
	var err error
	if !isInsideGitRepo(root) {
		fmt.Fprintf(os.Stderr, "warning: %s is not inside a git repo, using directory walk\n", root)
		files, err = scanWalk(ctx, root)
	} else {
		files, err = scanGit(ctx, root)
		if err != nil {
			files, err = scanWalk(ctx, root)
		}
	}
	if err != nil {
		return nil, err
	}

	slices.SortFunc(files, func(a, b FileInfo) int {
		return strings.Compare(a.Path, b.Path)
	})

	return files, nil
}

func scanGit(ctx context.Context, root string) ([]FileInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if inSkipDir(line) {
			continue
		}

		lang := LanguageFor(filepath.Ext(line))
		if lang == "" {
			continue
		}

		absPath := filepath.Join(root, line)
		if tooBig(absPath) || isBuildArtifact(line) {
			continue
		}

		files = append(files, FileInfo{Path: line, Language: lang})
	}

	return files, nil
}

func scanWalk(ctx context.Context, root string) ([]FileInfo, error) {
	var files []FileInfo

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			// Skip nested git repos and submodules
			if path != root {
				if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}

		lang := LanguageFor(filepath.Ext(path))
		if lang == "" {
			return nil
		}

		if tooBig(path) || isBuildArtifact(path) {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil //nolint:nilerr // skip if relative path can't be computed
		}

		files = append(files, FileInfo{Path: rel, Language: lang})
		return nil
	})

	return files, err
}
