package repomap

import (
	"bytes"
	"context"
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
// Returns nil if the directory is not inside a git repo.
// Falls back to a directory walk if `git ls-files` fails.
// cfg may be nil — nil means no path filtering.
func ScanFiles(ctx context.Context, root string, cfg *BlocklistConfig) ([]FileInfo, error) {
	if !isInsideGitRepo(root) {
		return nil, nil
	}

	files, err := scanGit(ctx, root, cfg)
	if err != nil {
		files, err = scanWalk(ctx, root, cfg)
		if err != nil {
			return nil, err
		}
	}

	slices.SortFunc(files, func(a, b FileInfo) int {
		return strings.Compare(a.Path, b.Path)
	})

	return files, nil
}

func scanGit(ctx context.Context, root string, cfg *BlocklistConfig) ([]FileInfo, error) {
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

		// line is already relative to root (git ls-files output).
		if cfg != nil {
			if cfg.ShouldExcludePath(line) {
				continue
			}
			if !cfg.ShouldIncludePath(line) {
				continue
			}
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

func scanWalk(ctx context.Context, root string, cfg *BlocklistConfig) ([]FileInfo, error) {
	var files []FileInfo

	err := filepath.WalkDir(root, func(fpath string, d fs.DirEntry, err error) error {
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
			if fpath != root {
				if _, err := os.Stat(filepath.Join(fpath, ".git")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}

		lang := LanguageFor(filepath.Ext(fpath))
		if lang == "" {
			return nil
		}

		if tooBig(fpath) || isBuildArtifact(fpath) {
			return nil
		}

		rel, err := filepath.Rel(root, fpath)
		if err != nil {
			return nil //nolint:nilerr // skip if relative path can't be computed
		}

		if cfg != nil {
			if cfg.ShouldExcludePath(rel) {
				return nil
			}
			if !cfg.ShouldIncludePath(rel) {
				return nil
			}
		}

		files = append(files, FileInfo{Path: rel, Language: lang})
		return nil
	})

	return files, err
}
