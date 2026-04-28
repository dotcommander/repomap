package repomap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ExecuteOptions configures a commit-execute run.
type ExecuteOptions struct {
	Root             string // repo root (default ".")
	PlanFile         string // path to CommitAnalysis JSON (required)
	Push             bool   // git push origin <branch> --follow-tags
	Tag              string // annotated tag to create at HEAD (semver)
	NoRelease        bool   // skip gh release create
	ReleaseNotesFrom string // --notes-start-tag for gh release create
	DryRun           bool   // print actions, mutate nothing
	JSON             bool   // machine-readable result on stdout
	SkipFix          bool   // bypass consolidation pass
}

// ExecuteResult is the JSON-serializable result of a successful execute run.
type ExecuteResult struct {
	Branch     string          `json:"branch"`
	Commits    []CommitRecord  `json:"commits"`
	Tag        *string         `json:"tag"`
	Pushed     bool            `json:"pushed"`
	ReleaseURL *string         `json:"release_url"`
	Postflight PostflightCheck `json:"postflight"`
}

// CommitRecord is one landed commit.
type CommitRecord struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
}

// PostflightCheck records the result of each postflight verification.
// Checks that are not applicable (e.g. TagRemote when --push was not set)
// are set to true so callers can use postflightOK() without special-casing.
type PostflightCheck struct {
	Clean     bool `json:"clean"`
	Convent   bool `json:"conventional"`
	TagLocal  bool `json:"tag_local"`
	TagRemote bool `json:"tag_remote"`
	Release   bool `json:"release"`
}

// execError carries an exit code alongside its message.
// Exit codes: 2=validation, 3=git failure, 4=push/release failed after commits landed.
type execError struct {
	code int
	msg  string
}

func (e execError) Error() string { return e.msg }

// ExecExitCode extracts the exit code from an execError, defaulting to 1.
func ExecExitCode(err error) int {
	if e, ok := err.(execError); ok {
		return e.code
	}
	return 1
}

// EncodeExecuteResult serializes an ExecuteResult for stdout.
func EncodeExecuteResult(r *ExecuteResult, pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(r, "", "  ")
	}
	return json.Marshal(r)
}

// ExecuteCommit loads the plan, validates it, consolidates groups, then
// executes git add/commit per group, then push/tag/release.
func ExecuteCommit(ctx context.Context, opts ExecuteOptions) (*ExecuteResult, error) {
	root, err := resolveRoot(opts.Root)
	if err != nil {
		return nil, err
	}
	analysis, err := loadAndValidatePlan(opts.PlanFile)
	if err != nil {
		return nil, execError{code: 2, msg: fmt.Sprintf("invalid plan: %v", err)}
	}
	groups := analysis.Groups
	if !opts.SkipFix {
		groups = ConsolidateGroups(groups)
	}
	return executeGroups(ctx, root, groups, opts)
}

// ExecuteFromGroups runs the commit pipeline directly from a validated slice of
// CommitGroups, bypassing the plan-file load path. Intended for commit finish,
// which already has groups in memory. opts.PlanFile and opts.SkipFix are ignored.
func ExecuteFromGroups(ctx context.Context, repoRoot string, groups []CommitGroup, opts ExecuteOptions) (*ExecuteResult, error) {
	root, err := resolveRoot(repoRoot)
	if err != nil {
		return nil, err
	}
	return executeGroups(ctx, root, groups, opts)
}

// executeGroups is the shared inner pipeline: validate → dry-run check →
// commit-loop → tag → push → release → postflight. Both ExecuteCommit and
// ExecuteFromGroups delegate here after resolving their root + groups.
func executeGroups(ctx context.Context, root string, groups []CommitGroup, opts ExecuteOptions) (*ExecuteResult, error) {
	if opts.Tag != "" {
		if err := ValidateTag(opts.Tag); err != nil {
			return nil, execError{code: 2, msg: err.Error()}
		}
	}
	for _, g := range groups {
		if err := ValidateConventionalMsg(g.SuggestedMsg); err != nil {
			return nil, execError{code: 2, msg: fmt.Sprintf("group %s: %v", g.ID, err)}
		}
	}
	if opts.DryRun {
		printDryRun(groups, opts)
		return &ExecuteResult{Tag: tagPtr(opts.Tag)}, nil
	}
	if err := verifyWorkspaceClean(ctx, root, groups); err != nil {
		return nil, execError{code: 3, msg: err.Error()}
	}
	branch, err := currentBranch(ctx, root)
	if err != nil {
		return nil, execError{code: 3, msg: fmt.Sprintf("get branch: %v", err)}
	}
	var landed []CommitRecord
	for _, g := range groups {
		sha, err := execCommit(ctx, root, g.Files, g.SuggestedMsg)
		if err != nil {
			return nil, execError{code: 3, msg: fmt.Sprintf("commit %q: %v", g.SuggestedMsg, err)}
		}
		landed = append(landed, CommitRecord{SHA: sha, Message: g.SuggestedMsg})
	}
	if opts.Tag != "" {
		if err := execTag(ctx, root, opts.Tag); err != nil {
			return nil, execError{code: 3, msg: fmt.Sprintf("tag %s: %v", opts.Tag, err)}
		}
	}
	pushed := false
	if opts.Push {
		if err := execPush(ctx, root, branch); err != nil {
			return buildPartialResult(branch, landed, opts, false, nil,
				fmt.Sprintf("push failed: %v", err))
		}
		pushed = true
	}
	var releaseURL *string
	if opts.Push && opts.Tag != "" && !opts.NoRelease {
		url, err := execRelease(ctx, root, opts.Tag, opts.ReleaseNotesFrom)
		if err != nil {
			return buildPartialResult(branch, landed, opts, pushed, nil,
				fmt.Sprintf("gh release failed: %v", err))
		}
		releaseURL = &url
	}
	pf := runPostflight(ctx, root, opts, pushed, releaseURL)
	result := &ExecuteResult{
		Branch:     branch,
		Commits:    landed,
		Tag:        tagPtr(opts.Tag),
		Pushed:     pushed,
		ReleaseURL: releaseURL,
		Postflight: pf,
	}
	if !postflightOK(pf) {
		return result, execError{code: 4, msg: "postflight verification failed (see result for details)"}
	}
	return result, nil
}

// loadAndValidatePlan reads and validates a CommitAnalysis JSON file.
func loadAndValidatePlan(path string) (*CommitAnalysis, error) {
	if path == "" {
		return nil, fmt.Errorf("--plan-file is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan file: %w", err)
	}
	var a CommitAnalysis
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("parse plan JSON: %w", err)
	}
	if err := checkSecrets(a.Secrets, a.Refs.Findings); err != nil {
		return nil, err
	}
	for _, g := range a.Groups {
		if len(g.Files) == 0 {
			return nil, fmt.Errorf("group %s has no files", g.ID)
		}
		if strings.TrimSpace(g.SuggestedMsg) == "" {
			return nil, fmt.Errorf("group %s has empty suggested_msg", g.ID)
		}
	}
	return &a, nil
}

// verifyWorkspaceClean ensures no dirty files exist outside the planned groups.
func verifyWorkspaceClean(ctx context.Context, root string, groups []CommitGroup) error {
	planned := make(map[string]bool)
	for _, g := range groups {
		for _, f := range g.Files {
			planned[f] = true
		}
	}
	porcelain, err := gitOutput(ctx, root, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	var unplanned []string
	for _, line := range strings.Split(strings.TrimRight(porcelain, "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		if !planned[path] {
			unplanned = append(unplanned, path)
		}
	}
	if len(unplanned) > 0 {
		return fmt.Errorf("dirty files not in any commit group (stage or stash first):\n  %s",
			strings.Join(unplanned, "\n  "))
	}
	return nil
}

// runPostflight verifies workspace cleanliness, conventional message, tag, and release.
// Not-applicable checks are set true so postflightOK is a simple all-true test.
func runPostflight(ctx context.Context, root string, opts ExecuteOptions, pushed bool, releaseURL *string) PostflightCheck {
	pf := PostflightCheck{TagLocal: true, TagRemote: true, Release: true}
	if out, err := gitOutput(ctx, root, "status", "--porcelain"); err == nil {
		pf.Clean = strings.TrimSpace(out) == ""
	}
	if out, err := gitOutput(ctx, root, "log", "-1", "--pretty=%B"); err == nil {
		if lines := strings.Split(out, "\n"); len(lines) > 0 {
			pf.Convent = conventionalSubjectRe.MatchString(strings.TrimSpace(lines[0]))
		}
	}
	if opts.Tag != "" {
		if out, err := gitOutput(ctx, root, "describe", "--tags", "--exact-match", "HEAD"); err == nil {
			pf.TagLocal = strings.TrimSpace(out) == opts.Tag
		} else {
			pf.TagLocal = false
		}
		if pushed {
			if out, err := gitOutput(ctx, root, "ls-remote", "--tags", "origin", opts.Tag); err == nil {
				pf.TagRemote = strings.TrimSpace(out) != ""
			} else {
				pf.TagRemote = false
			}
		}
	}
	if releaseURL != nil {
		cmd := exec.CommandContext(ctx, "gh", "release", "view", opts.Tag, "--json", "url")
		cmd.Dir = root
		pf.Release = cmd.Run() == nil
	}
	return pf
}

func postflightOK(pf PostflightCheck) bool {
	return pf.Clean && pf.Convent && pf.TagLocal && pf.TagRemote && pf.Release
}
