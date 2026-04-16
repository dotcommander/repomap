package repomap

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
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
