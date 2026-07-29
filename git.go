package repomap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// gitHeadSHA returns the full SHA of HEAD. Returns an error (not empty string)
// so callers can distinguish "git call failed" from "clean repo with no commits".
func gitHeadSHA(ctx context.Context, root string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// cacheStatusSnapshot is one coherent Git view used to decide whether an
// on-disk cache describes the current worktree. Its digest deliberately
// ignores index state: staging identical bytes must not invalidate a cache.
type cacheStatusSnapshot struct {
	headSHA string
	changes []cacheWorktreeChange
	digest  string
}

type cacheWorktreeChange struct {
	path      string
	oldPath   string
	untracked bool
	renamed   bool
}

// gitStatusSnapshot obtains HEAD and worktree changes in one Git invocation.
// A missing HEAD, conflict, malformed porcelain, or unreadable relevant file
// returns an error so callers fall back to a full build.
func (m *Map) gitStatusSnapshot(ctx context.Context) (cacheStatusSnapshot, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", m.root, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all", "--renames")
	out, err := cmd.Output()
	if err != nil {
		return cacheStatusSnapshot{}, err
	}
	snapshot, err := parseCacheStatusSnapshot(string(out))
	if err != nil {
		return cacheStatusSnapshot{}, err
	}
	snapshot.changes, err = normalizeCacheStatusPaths(m.root, snapshot.changes)
	if err != nil {
		return cacheStatusSnapshot{}, err
	}
	digest, err := m.cacheWorktreeDigest(snapshot.changes)
	if err != nil {
		return cacheStatusSnapshot{}, err
	}
	snapshot.digest = digest
	return snapshot, nil
}

// parseCacheStatusSnapshot accepts only the porcelain-v2 record types needed
// by the cache. Unlike the commit-analysis parser, it rejects uncertain state
// because cache reuse must be conservative.
func parseCacheStatusSnapshot(raw string) (cacheStatusSnapshot, error) {
	var snapshot cacheStatusSnapshot
	records := splitNUL(raw)
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			return cacheStatusSnapshot{}, fmt.Errorf("empty status record")
		}
		switch record[0] {
		case '#':
			if strings.HasPrefix(record, "# branch.oid ") {
				oid := strings.TrimPrefix(record, "# branch.oid ")
				if !isGitObjectID(oid) {
					return cacheStatusSnapshot{}, fmt.Errorf("invalid branch oid %q", oid)
				}
				snapshot.headSHA = oid
			}
		case '1':
			fields := strings.SplitN(record, " ", 9)
			if len(fields) != 9 || fields[0] != "1" || !validPorcelainXY(fields[1]) || !validGitPath(fields[8]) {
				return cacheStatusSnapshot{}, fmt.Errorf("malformed ordinary status record %q", record)
			}
			snapshot.changes = append(snapshot.changes, cacheWorktreeChange{path: fields[8]})
		case '2':
			fields := strings.SplitN(record, " ", 10)
			if len(fields) != 10 || fields[0] != "2" || !validPorcelainXY(fields[1]) || !validGitPath(fields[9]) || i+1 >= len(records) || !validGitPath(records[i+1]) {
				return cacheStatusSnapshot{}, fmt.Errorf("malformed rename status record %q", record)
			}
			snapshot.changes = append(snapshot.changes, cacheWorktreeChange{
				path: fields[9], oldPath: records[i+1], renamed: true,
			})
			i++
		case '?':
			if !strings.HasPrefix(record, "? ") || !validGitPath(record[2:]) {
				return cacheStatusSnapshot{}, fmt.Errorf("malformed untracked status record %q", record)
			}
			snapshot.changes = append(snapshot.changes, cacheWorktreeChange{path: record[2:], untracked: true})
		case 'u':
			return cacheStatusSnapshot{}, fmt.Errorf("unmerged status record %q", record)
		case '!':
			if !strings.HasPrefix(record, "! ") || !validGitPath(record[2:]) {
				return cacheStatusSnapshot{}, fmt.Errorf("malformed ignored status record %q", record)
			}
		default:
			return cacheStatusSnapshot{}, fmt.Errorf("unknown status record %q", record)
		}
	}
	if snapshot.headSHA == "" {
		return cacheStatusSnapshot{}, fmt.Errorf("status missing branch oid")
	}
	return snapshot, nil
}

func isGitObjectID(value string) bool {
	if len(value) < 7 || value == "(initial)" {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func validPorcelainXY(value string) bool {
	return len(value) == 2 && value[0] != ' ' && value[1] != ' '
}

func validGitPath(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// normalizeCacheStatusPaths converts porcelain-v2's repository-root-relative
// paths into Map-root-relative paths. Changes outside a nested Map root do not
// affect its cache; cross-boundary renames retain whichever side is inside.
func normalizeCacheStatusPaths(mapRoot string, changes []cacheWorktreeChange) ([]cacheWorktreeChange, error) {
	gitRoot, err := containingGitWorktreeRoot(mapRoot)
	if err != nil {
		return nil, err
	}
	out := make([]cacheWorktreeChange, 0, len(changes))
	for _, change := range changes {
		path, pathInside, pathErr := gitPathRelativeToMap(gitRoot, mapRoot, change.path)
		if pathErr != nil {
			return nil, pathErr
		}
		oldPath, oldInside, oldErr := gitPathRelativeToMap(gitRoot, mapRoot, change.oldPath)
		if oldErr != nil {
			return nil, oldErr
		}
		if !pathInside && !oldInside {
			continue
		}
		change.path = path
		change.oldPath = oldPath
		out = append(out, change)
	}
	return out, nil
}

func containingGitWorktreeRoot(root string) (string, error) {
	dir, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve map root: %w", err)
	}
	for {
		if _, statErr := os.Lstat(filepath.Join(dir, ".git")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("map root %q is not inside a git worktree", root)
		}
		dir = parent
	}
}

func gitPathRelativeToMap(gitRoot, mapRoot, path string) (string, bool, error) {
	if path == "" {
		return "", false, nil
	}
	absMapRoot, err := filepath.Abs(mapRoot)
	if err != nil {
		return "", false, fmt.Errorf("resolve map root: %w", err)
	}
	rel, err := filepath.Rel(absMapRoot, filepath.Join(gitRoot, filepath.FromSlash(path)))
	if err != nil {
		return "", false, fmt.Errorf("normalize git path %q: %w", path, err)
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, nil
	}
	return filepath.ToSlash(rel), true, nil
}

func (m *Map) cacheWorktreeDigest(changes []cacheWorktreeChange) (string, error) {
	paths := make(map[string]struct{}, len(changes)*2)
	for _, change := range changes {
		if m.cacheRelevantPath(change.path) {
			paths[change.path] = struct{}{}
		}
		if change.oldPath != "" && m.cacheRelevantPath(change.oldPath) {
			paths[change.oldPath] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(paths))
	for path := range paths {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)

	h := sha256.New()
	_, _ = h.Write([]byte("repomap-cache-worktree-v1\x00"))
	for _, path := range sorted {
		contents, err := os.ReadFile(filepath.Join(m.root, path))
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("read changed path %q: %w", path, err)
		}
		if os.IsNotExist(err) {
			_, _ = fmt.Fprintf(h, "%d:%s:-1:", len(path), path)
			continue
		}
		_, _ = fmt.Fprintf(h, "%d:%s:%d:", len(path), path, len(contents))
		_, _ = h.Write(contents)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func cacheCleanWorktreeDigest() string {
	h := sha256.New()
	_, _ = h.Write([]byte("repomap-cache-worktree-v1\x00"))
	return hex.EncodeToString(h.Sum(nil))
}

func (m *Map) cacheRelevantPath(path string) bool {
	if path == configFileName {
		return true
	}
	if inSkipDir(path) || isBuildArtifact(path) {
		return false
	}
	base := filepath.Base(path)
	if base == "go.mod" || base == "go.sum" || base == "go.work" {
		return true
	}
	if m.blocklist != nil && (m.blocklist.ShouldExcludePath(path) || !m.blocklist.ShouldIncludePath(path)) {
		return false
	}
	if LanguageFor(filepath.Ext(path)) != "" {
		return true
	}
	return false
}

// snapshotChangedFiles translates current worktree state into the categories
// consumed by prepareIncremental. A rename is deliberately old-delete plus
// new-add so cached symbols at the old path cannot survive.
func (m *Map) snapshotChangedFiles(snapshot cacheStatusSnapshot) (added, modified, deleted []string, err error) {
	for _, change := range snapshot.changes {
		if change.renamed {
			if m.cacheRelevantPath(change.oldPath) {
				deleted = append(deleted, change.oldPath)
			}
			if m.cacheRelevantPath(change.path) {
				added = append(added, change.path)
			}
			continue
		}
		if !m.cacheRelevantPath(change.path) {
			continue
		}
		if change.untracked {
			added = append(added, change.path)
			continue
		}
		if _, statErr := os.Lstat(filepath.Join(m.root, change.path)); statErr != nil {
			if os.IsNotExist(statErr) {
				deleted = append(deleted, change.path)
				continue
			}
			return nil, nil, nil, fmt.Errorf("stat changed path %q: %w", change.path, statErr)
		}
		modified = append(modified, change.path)
	}
	return dedupePaths(added), dedupePaths(modified), dedupePaths(deleted), nil
}

// gitCommittedChangedFiles issues the one committed-diff command permitted by
// the HEAD-moved incremental path. Worktree changes are supplied separately by
// the already-captured status snapshot.
func gitCommittedChangedFiles(ctx context.Context, root, sinceSHA string) (added, modified, deleted []string, err error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "--name-status", "-z", "-M", sinceSHA, "HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, nil, nil, err
	}
	added, modified, deleted, err = parseDiffNameStatusStrict(out.String())
	if err != nil {
		return nil, nil, nil, err
	}
	gitRoot, err := containingGitWorktreeRoot(root)
	if err != nil {
		return nil, nil, nil, err
	}
	normalize := func(paths []string) ([]string, error) {
		normalized := make([]string, 0, len(paths))
		for _, path := range paths {
			rel, inside, pathErr := gitPathRelativeToMap(gitRoot, root, path)
			if pathErr != nil {
				return nil, pathErr
			}
			if inside {
				normalized = append(normalized, rel)
			}
		}
		return normalized, nil
	}
	if added, err = normalize(added); err != nil {
		return nil, nil, nil, err
	}
	if modified, err = normalize(modified); err != nil {
		return nil, nil, nil, err
	}
	if deleted, err = normalize(deleted); err != nil {
		return nil, nil, nil, err
	}
	return added, modified, deleted, nil
}

func parseDiffNameStatusStrict(raw string) (added, modified, deleted []string, err error) {
	tokens := splitNUL(raw)
	for i := 0; i < len(tokens); i++ {
		status := tokens[i]
		if status == "" {
			return nil, nil, nil, fmt.Errorf("empty diff status")
		}
		needPath := func() (string, error) {
			if i+1 >= len(tokens) || !validGitPath(tokens[i+1]) {
				return "", fmt.Errorf("malformed diff record %q", status)
			}
			i++
			return tokens[i], nil
		}
		switch status[0] {
		case 'A':
			path, pathErr := needPath()
			if pathErr != nil {
				return nil, nil, nil, pathErr
			}
			added = append(added, path)
		case 'M', 'T':
			path, pathErr := needPath()
			if pathErr != nil {
				return nil, nil, nil, pathErr
			}
			modified = append(modified, path)
		case 'D':
			path, pathErr := needPath()
			if pathErr != nil {
				return nil, nil, nil, pathErr
			}
			deleted = append(deleted, path)
		case 'R', 'C':
			oldPath, pathErr := needPath()
			if pathErr != nil {
				return nil, nil, nil, pathErr
			}
			newPath, pathErr := needPath()
			if pathErr != nil {
				return nil, nil, nil, pathErr
			}
			deleted = append(deleted, oldPath)
			added = append(added, newPath)
		default:
			return nil, nil, nil, fmt.Errorf("unknown diff status %q", status)
		}
	}
	return dedupePaths(added), dedupePaths(modified), dedupePaths(deleted), nil
}

// gitChangedFiles returns added, modified, and deleted paths between sinceSHA
// and HEAD, plus untracked files. Paths are relative to root, matching the
// FileInfo.Path convention from scanner.go.
//
// Untracked files (git ls-files --others --exclude-standard) are treated as
// "added" — they respect .gitignore and represent files new since the cache
// was written. This catches the common edit-without-commit workflow.
//
// Renames are reported as delete(old) + add(new) via --diff-filter semantics.
// `git diff --name-status -M` would give R entries; we use the simpler
// status-letter form and let callers re-parse the new path.
func gitChangedFiles(ctx context.Context, root, sinceSHA string) (added, modified, deleted []string, err error) {
	// Committed changes: A (added), M (modified), D (deleted), R (renamed -> treat as D old + A new).
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "--name-status", "-z", sinceSHA, "HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, nil, nil, err
	}
	parseDiffNameStatus(out.String(), &added, &modified, &deleted)

	// Worktree changes not yet committed: unstaged + staged vs HEAD.
	cmd = exec.CommandContext(ctx, "git", "-C", root, "diff", "--name-status", "-z", "HEAD")
	out.Reset()
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil { // best-effort
		parseDiffNameStatus(out.String(), &added, &modified, &deleted)
	}

	// Untracked (not ignored) — counted as added.
	cmd = exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--others", "--exclude-standard", "-z")
	out.Reset()
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		for _, p := range splitNUL(out.String()) {
			if p != "" {
				added = append(added, p)
			}
		}
	}

	added = dedupePaths(added)
	modified = dedupePaths(modified)
	deleted = dedupePaths(deleted)
	return added, modified, deleted, nil
}

// parseDiffNameStatus parses the NUL-delimited output of
// `git diff --name-status -z`. Each record is STATUS\0PATH (optionally with a
// second PATH for renames R/C). Status letters: A, M, D, T (type change,
// treated as modify), R<score>, C<score>.
func parseDiffNameStatus(raw string, added, modified, deleted *[]string) {
	tokens := splitNUL(raw)
	for i := 0; i < len(tokens); i++ {
		status := tokens[i]
		if status == "" {
			continue
		}
		switch status[0] {
		case 'A':
			if i+1 < len(tokens) {
				*added = append(*added, tokens[i+1])
				i++
			}
		case 'M', 'T':
			if i+1 < len(tokens) {
				*modified = append(*modified, tokens[i+1])
				i++
			}
		case 'D':
			if i+1 < len(tokens) {
				*deleted = append(*deleted, tokens[i+1])
				i++
			}
		case 'R', 'C':
			// Format: R<score>\0<old>\0<new>. Treat as delete(old) + add(new).
			if i+2 < len(tokens) {
				*deleted = append(*deleted, tokens[i+1])
				*added = append(*added, tokens[i+2])
				i += 2
			}
		}
	}
}

func splitNUL(s string) []string {
	s = strings.TrimRight(s, "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}

func dedupePaths(in []string) []string {
	if len(in) < 2 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, p := range in {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// joinAbs converts a repo-relative path to an absolute path rooted at root.
// Thin wrapper so incremental.go stays focused on orchestration.
func joinAbs(root, rel string) string {
	return filepath.Join(root, rel)
}
