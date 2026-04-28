package repomap

// commit_verify.go — Cross-repo and self-verification for commit prep/finish.
//
// Two entry points:
//   - CrossRepoVerify: checks git porcelain status across multiple repos.
//   - SelfVerify: mode-aware check (local | full | auto) for the current repo.

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
)

// selfVerifyConventionalRe validates conventional commit format:
// type(scope)?: subject (≤72 chars)
// Examples: "feat(search): add fuzzy matcher", "fix: prevent race"
var selfVerifyConventionalRe = regexp.MustCompile(
	`^(feat|fix|docs|chore|refactor|test|perf|style|build|ci|revert)(\([^)]+\))?: .{1,72}$`,
)

// RepoStatus records the porcelain status of a single repo.
type RepoStatus struct {
	Repo  string   `json:"repo"`
	Dirty []string `json:"dirty"` // lines from git status --porcelain; empty = clean
}

// VerifyResult records the outcome of a self-verify run.
type VerifyResult struct {
	Mode              string `json:"mode"` // "local" | "full"
	OK                bool   `json:"ok"`
	LastCommitSubject string `json:"last_commit_subject,omitempty"` // local mode
	Tag               string `json:"tag,omitempty"`                  // full mode
	ReleaseURL        string `json:"release_url,omitempty"`          // full mode
	FailureDetail     string `json:"failure_detail,omitempty"`
}

// CrossRepoVerify checks git porcelain status across multiple repos.
func CrossRepoVerify(ctx context.Context, sessionRepos []string) (results []RepoStatus, allClean bool) {
	allClean = true
	for _, repo := range sessionRepos {
		cmd := exec.CommandContext(ctx, "git", "-C", repo, "status", "--porcelain")
		out, err := cmd.Output()
		if err != nil {
			allClean = false
			results = append(results, RepoStatus{
				Repo:  repo,
				Dirty: []string{"(error: " + err.Error() + ")"},
			})
			continue
		}

		lines := parsePorcelainLines(string(out))
		if len(lines) > 0 {
			allClean = false
		}
		results = append(results, RepoStatus{
			Repo:  repo,
			Dirty: lines,
		})
	}
	return results, allClean
}

// SelfVerify performs mode-aware verification of the current repo.
func SelfVerify(ctx context.Context, repoRoot, mode string) (VerifyResult, error) {
	if mode == "" || mode == "auto" {
		mode = autoDetectMode(ctx, repoRoot)
	}

	switch mode {
	case "local":
		return selfVerifyLocal(ctx, repoRoot)
	case "full":
		return selfVerifyFull(ctx, repoRoot)
	default:
		return VerifyResult{Mode: mode, FailureDetail: "unknown mode"}, nil
	}
}

// autoDetectMode infers the mode based on repo state.
func autoDetectMode(ctx context.Context, repoRoot string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "describe", "--tags", "--exact-match", "HEAD")
	if err := cmd.Run(); err == nil {
		return "full"
	}
	return "local"
}

// selfVerifyLocal checks that the last commit has a valid conventional commit message.
func selfVerifyLocal(ctx context.Context, repoRoot string) (VerifyResult, error) {
	r := VerifyResult{Mode: "local"}

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "log", "-1", "--pretty=%s")
	out, err := cmd.Output()
	if err != nil {
		r.FailureDetail = "failed to get last commit: " + err.Error()
		return r, nil
	}

	subject := strings.TrimSpace(string(out))
	if subject == "" {
		r.FailureDetail = "no commits in repo"
		return r, nil
	}

	if !selfVerifyConventionalRe.MatchString(subject) {
		r.FailureDetail = "last commit does not follow conventional format: " + subject
		return r, nil
	}

	r.OK = true
	r.LastCommitSubject = subject
	return r, nil
}

// selfVerifyFull checks that HEAD is tagged and the tag exists on GitHub.
func selfVerifyFull(ctx context.Context, repoRoot string) (VerifyResult, error) {
	r := VerifyResult{Mode: "full"}

	// Get exact-match tag at HEAD.
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "describe", "--tags", "--exact-match", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		r.FailureDetail = "HEAD is not tagged"
		return r, nil
	}
	tag := strings.TrimSpace(string(out))
	r.Tag = tag

	// Tag must exist on origin.
	cmd = exec.CommandContext(ctx, "git", "-C", repoRoot, "ls-remote", "--tags", "origin", tag)
	out, err = cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		r.FailureDetail = "tag " + tag + " not found on origin"
		return r, nil
	}

	// GitHub release must exist.
	ghCmd := exec.CommandContext(ctx, "gh", "release", "view", tag, "--json", "url", "-q", ".url")
	ghCmd.Dir = repoRoot
	out, err = ghCmd.Output()
	if err != nil {
		r.FailureDetail = "no GitHub release for " + tag
		return r, nil
	}

	r.OK = true
	r.ReleaseURL = strings.TrimSpace(string(out))
	return r, nil
}

// parsePorcelainLines splits git status --porcelain output into non-empty lines.
func parsePorcelainLines(raw string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
