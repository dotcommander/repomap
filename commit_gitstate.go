package repomap

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// gitState aggregates everything we shell out to git for. Built once per
// analyze invocation; downstream phases (grouping, messages, secrets) operate
// on this snapshot.
type gitState struct {
	Root        string
	Branch      string
	Files       []fileChange
	HistoryRaw  string                    // last 20 commit subjects, one per line
	CoChange    map[string]map[string]int // file -> file -> co-commit count (last 500 commits)
	Tags        []string                  // descending semver
	HasUpstream bool
	OriginURL   string // origin remote URL; empty if no origin
	Visibility  string // public | private | none | unknown
}

// collectGitState shells the git commands we need. Returns an error only for
// fatal issues (not a git repo, git missing); empty changesets are normal.
func collectGitState(ctx context.Context, root string) (*gitState, error) {
	gs := &gitState{
		Root:     root,
		CoChange: make(map[string]map[string]int),
	}

	// Branch (best-effort).
	if out, err := runGit(ctx, root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		gs.Branch = strings.TrimSpace(out)
	}

	// Upstream presence.
	if _, err := runGit(ctx, root, "rev-parse", "--abbrev-ref", "@{u}"); err == nil {
		gs.HasUpstream = true
	}

	// Status (porcelain v2 with rename detection).
	statusOut, err := runGit(ctx, root, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--renames")
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	files, err := parsePorcelainV2(statusOut)
	if err != nil {
		return nil, fmt.Errorf("parse status: %w", err)
	}
	gs.Files = files

	// Per-file numstat for churn (covers staged + unstaged together via HEAD comparison).
	if len(gs.Files) > 0 {
		if churn, err := collectChurn(ctx, root); err == nil {
			applyChurn(gs.Files, churn)
		}
	}

	// History sample (commit-style detection happens in analyzer).
	if out, err := runGit(ctx, root, "log", "-n", "20", "--pretty=format:%s"); err == nil {
		gs.HistoryRaw = out
	}

	// Co-change graph from last 500 commits, scoped to dirty files only.
	dirty := dirtySet(gs.Files)
	if len(dirty) >= 2 {
		gs.CoChange = collectCoChange(ctx, root, dirty)
	}

	// Tags (descending semver, top 5).
	if out, err := runGit(ctx, root, "tag", "--sort=-version:refname"); err == nil {
		for i, line := range splitLines(out) {
			if i >= 5 {
				break
			}
			gs.Tags = append(gs.Tags, line)
		}
	}

	// Origin + visibility. Drives Finding.DefaultAction strictness: no remote =
	// personal repo (lenient); public remote = strict; unknown = strict.
	gs.OriginURL, gs.Visibility = detectVisibility(ctx, root)

	return gs, nil
}

// detectVisibility classifies the origin remote. Logic:
//   - no origin                         → ("", "none")
//   - origin exists, `gh` authenticated → probe gh repo view for github.com URLs
//   - otherwise                         → (url, "unknown")  — caller treats as strict
//
// Network-light: gh probe is skipped when origin is not github.com or gh is
// missing; both paths short-circuit to "unknown" so analyze never stalls on
// flaky network.
func detectVisibility(ctx context.Context, root string) (url, vis string) {
	out, err := runGit(ctx, root, "remote", "get-url", "origin")
	if err != nil {
		return "", "none"
	}
	url = strings.TrimSpace(out)
	if url == "" {
		return "", "none"
	}
	if !isGitHubURL(url) {
		return url, "unknown"
	}
	slug := githubSlug(url)
	if slug == "" {
		return url, "unknown"
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return url, "unknown"
	}
	cmd := exec.CommandContext(ctx, "gh", "repo", "view", slug, "--json", "visibility", "-q", ".visibility")
	raw, err := cmd.Output()
	if err != nil {
		return url, "unknown"
	}
	switch strings.ToLower(strings.TrimSpace(string(raw))) {
	case "public":
		return url, "public"
	case "private", "internal":
		return url, "private"
	}
	return url, "unknown"
}

// isGitHubURL returns true for SSH or HTTPS origins on github.com.
func isGitHubURL(url string) bool {
	return strings.Contains(url, "github.com:") || strings.Contains(url, "github.com/")
}

// githubSlug extracts "owner/repo" from a github.com URL.
// Handles: git@github.com:owner/repo.git, https://github.com/owner/repo(.git),
// ssh://git@github.com/owner/repo.
func githubSlug(url string) string {
	rest := url
	switch {
	case strings.HasPrefix(rest, "git@github.com:"):
		rest = strings.TrimPrefix(rest, "git@github.com:")
	case strings.Contains(rest, "github.com/"):
		_, rest, _ = strings.Cut(rest, "github.com/")
	default:
		return ""
	}
	rest = strings.TrimSuffix(rest, ".git")
	rest = strings.TrimSuffix(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// runGit invokes git with the given args inside root, returns stdout.
func runGit(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// parsePorcelainV2 parses NUL-separated porcelain v2 output. Format reference:
// https://git-scm.com/docs/git-status#_porcelain_format_version_2
//
// Record kinds:
//
//	1 XY ...     ordinary changed entry
//	2 XY ... <sep> orig-path     renamed/copied entry (orig path is in the *next* NUL-separated record)
//	u XY ...     unmerged entry
//	? path       untracked
//	! path       ignored (filtered out)
func parsePorcelainV2(raw string) ([]fileChange, error) {
	var out []fileChange
	// porcelain=v2 -z separates records by NUL; rename records have the original
	// path as a SECOND NUL-separated token (not embedded). We walk records with
	// an index so we can pull the next one for renames.
	records := strings.Split(strings.TrimRight(raw, "\x00"), "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '1':
			fc, err := parseV2Ordinary(rec)
			if err != nil {
				return nil, err
			}
			out = append(out, fc)
		case '2':
			// Rename/copy: this record has the new path; the next record is the orig path.
			if i+1 >= len(records) {
				return nil, fmt.Errorf("rename record missing orig path: %q", rec)
			}
			fc, err := parseV2Rename(rec, records[i+1])
			if err != nil {
				return nil, err
			}
			out = append(out, fc)
			i++ // consume orig-path record
		case '?':
			// Untracked: "? path"
			if len(rec) > 2 {
				out = append(out, fileChange{
					Path:        rec[2:],
					Status:      "?",
					IndexStatus: "?",
				})
			}
		case 'u':
			// Unmerged: treat as conflict — report path, leave status to caller.
			fields := strings.SplitN(rec, " ", 11)
			if len(fields) >= 11 {
				out = append(out, fileChange{
					Path:        fields[10],
					Status:      "U",
					IndexStatus: "U",
				})
			}
		case '!':
			// Ignored — skip.
		}
	}
	return out, nil
}

// parseV2Ordinary parses a "1 XY sub mH mI mW hH hI path" record.
func parseV2Ordinary(rec string) (fileChange, error) {
	fields := strings.SplitN(rec, " ", 9)
	if len(fields) < 9 {
		return fileChange{}, fmt.Errorf("ordinary record short: %q", rec)
	}
	xy := fields[1]
	if len(xy) < 2 {
		return fileChange{}, fmt.Errorf("xy short: %q", xy)
	}
	return fileChange{
		Path:        fields[8],
		IndexStatus: string(xy[0]),
		Status:      string(xy[1]),
	}, nil
}

// parseV2Rename parses a "2 XY sub mH mI mW hH hI Rscore path" record.
// The orig path is in `origPath` (the *next* NUL-separated record).
func parseV2Rename(rec, origPath string) (fileChange, error) {
	fields := strings.SplitN(rec, " ", 10)
	if len(fields) < 10 {
		return fileChange{}, fmt.Errorf("rename record short: %q", rec)
	}
	xy := fields[1]
	if len(xy) < 2 {
		return fileChange{}, fmt.Errorf("xy short: %q", xy)
	}
	return fileChange{
		Path:        fields[9],
		OldPath:     origPath,
		IndexStatus: string(xy[0]),
		Status:      string(xy[1]),
	}, nil
}

// collectChurn returns added/removed line counts per file, combining staged and
// unstaged + untracked (untracked counted as fully added).
func collectChurn(ctx context.Context, root string) (map[string][2]int, error) {
	churn := make(map[string][2]int)

	// Staged + unstaged via single HEAD comparison.
	if out, err := runGit(ctx, root, "diff", "HEAD", "--numstat", "-M70"); err == nil {
		mergeNumstat(churn, out)
	} else {
		// HEAD may not exist (initial commit). Fall back to cached + worktree separately.
		if cached, e2 := runGit(ctx, root, "diff", "--cached", "--numstat", "-M70"); e2 == nil {
			mergeNumstat(churn, cached)
		}
		if worktree, e2 := runGit(ctx, root, "diff", "--numstat", "-M70"); e2 == nil {
			mergeNumstat(churn, worktree)
		}
	}
	return churn, nil
}

// mergeNumstat parses `git diff --numstat` lines into the churn map.
// Format: "<added>\t<removed>\t<path>" (binary files report "-\t-\tpath").
func mergeNumstat(out map[string][2]int, raw string) {
	for _, line := range splitLines(raw) {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		added, _ := strconv.Atoi(fields[0])
		removed, _ := strconv.Atoi(fields[1])
		// Renamed entries appear as "old => {dir} => new"; use the LAST path token.
		path := normalizeNumstatPath(fields[2])
		cur := out[path]
		out[path] = [2]int{cur[0] + added, cur[1] + removed}
	}
}

// normalizeNumstatPath collapses "old => new" or "{a => b}/file" into the new
// path so downstream lookups match git status's post-rename path.
func normalizeNumstatPath(p string) string {
	if !strings.Contains(p, "=>") {
		return p
	}
	// Brace form: "src/{old.go => new.go}"
	if open := strings.Index(p, "{"); open >= 0 {
		if close := strings.Index(p[open:], "}"); close > 0 {
			inner := p[open+1 : open+close]
			parts := strings.SplitN(inner, " => ", 2)
			if len(parts) == 2 {
				return p[:open] + strings.TrimSpace(parts[1]) + p[open+close+1:]
			}
		}
	}
	// Plain form: "old/path => new/path"
	parts := strings.SplitN(p, " => ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return p
}

// applyChurn populates Added/Removed on each fileChange from the churn map.
func applyChurn(files []fileChange, churn map[string][2]int) {
	for i := range files {
		c := churn[files[i].Path]
		files[i].Added = c[0]
		files[i].Removed = c[1]
	}
}

// dirtySet returns paths of all files in the changeset, indexed for O(1) lookup
// during co-change graph construction.
func dirtySet(files []fileChange) map[string]bool {
	s := make(map[string]bool, len(files))
	for _, f := range files {
		s[f.Path] = true
	}
	return s
}

// collectCoChange walks the last 500 commits and counts pairwise co-occurrence
// of dirty files within each commit's name-only file list. Only edges between
// dirty files are returned (no point recording co-change for files we're not
// committing).
func collectCoChange(ctx context.Context, root string, dirty map[string]bool) map[string]map[string]int {
	out := make(map[string]map[string]int)
	// Format: per-commit blank-line-separated, name-only file paths.
	// `--name-only` already gives one filename per line; we use a marker for boundaries.
	const marker = "===COMMIT==="
	raw, err := runGit(ctx, root, "log", "-n", "500",
		"--pretty=format:"+marker, "--name-only")
	if err != nil {
		return out
	}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var current []string
	flush := func() {
		// Filter to dirty files only, then count pairs.
		var hits []string
		for _, p := range current {
			if dirty[p] {
				hits = append(hits, p)
			}
		}
		current = current[:0]
		for i := 0; i < len(hits); i++ {
			for j := i + 1; j < len(hits); j++ {
				a, b := hits[i], hits[j]
				if out[a] == nil {
					out[a] = make(map[string]int)
				}
				if out[b] == nil {
					out[b] = make(map[string]int)
				}
				out[a][b]++
				out[b][a]++
			}
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == marker {
			flush()
			continue
		}
		if line == "" {
			continue
		}
		current = append(current, line)
	}
	flush()
	return out
}

// splitLines splits on \n and discards empty trailing lines.
func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// gitShowAt returns the contents of `path` at revision `rev` (or "HEAD"). Used
// by commit_messages.go to extract the pre-change parser snapshot for symbol
// diffing. Returns ("", nil) for files that didn't exist at that revision.
func gitShowAt(ctx context.Context, root, rev, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "show", rev+":"+path)
	out, err := cmd.Output()
	if err != nil {
		// Most likely "fatal: path X exists on disk, but not in HEAD" — file is new.
		return "", nil
	}
	return string(out), nil
}

// collectFullDiff dumps the entire HEAD-vs-working-tree diff to a file. Used
// by analyze.go to populate refs.diffs for low-confidence agent inspection.
func collectFullDiff(ctx context.Context, root, outPath string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "HEAD", "-M70", "--no-color")
	out, err := cmd.Output()
	if err != nil {
		// HEAD may not exist; fall back to combined cached + worktree.
		cached, _ := runGit(ctx, root, "diff", "--cached", "-M70", "--no-color")
		work, _ := runGit(ctx, root, "diff", "-M70", "--no-color")
		out = []byte(cached + work)
	}
	return writeFile(outPath, out)
}

// untrackedConfigContent dumps untracked config/markdown files (full content)
// to a file for Phase 2 LLM review of untracked content.
func untrackedConfigContent(ctx context.Context, root string, files []fileChange, outPath string) error {
	var sb strings.Builder
	for _, f := range files {
		if f.Status != "?" || !f.IsConfig {
			continue
		}
		content, err := readFileBounded(filepath.Join(root, f.Path), 64*1024)
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "=== %s ===\n%s\n", f.Path, content)
	}
	return writeFile(outPath, []byte(sb.String()))
}

// writeFile creates parent dirs and writes data atomically. Thin wrapper over
// the existing atomicWriteFile so commit code reads naturally.
func writeFile(path string, data []byte) error {
	return atomicWriteFile(path, data, 0o600)
}

// readFileBounded reads up to maxBytes from path. Returns truncated content
// with a trailing "\n... [TRUNCATED]\n" marker if the file exceeded the cap.
func readFileBounded(path string, maxBytes int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > maxBytes {
		return string(data[:maxBytes]) + "\n... [TRUNCATED]\n", nil
	}
	return string(data), nil
}
