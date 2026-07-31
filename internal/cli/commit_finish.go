package cli

// commit_finish.go — `repomap commit finish` subcommand.
//
// Loads the PrepState written by `commit prep`, applies any LLM decisions,
// then either runs `just release <bump>` (Justfile path) or calls
// internal/commit.ExecuteFromGroups (standard path), followed by cross-repo and
// self-verification.
//
// Exit codes:
//   0  passed
//   2  plan/decision validation error
//   3  git/execute error
//   4  push/release error
//   5  verify failure
//
// I/O helpers (emit, fatal, build, parse, bumpLevel, runJustRelease) → commit_finish_io.go.

import (
	"context"
	"fmt"
	"io"

	commitflow "github.com/dotcommander/repomap/internal/commit"
)

// finishDecisions is the JSON schema accepted by --decisions.
type finishDecisions struct {
	ReviewDecisions []commitflow.ReviewDecision `json:"review_decisions"`
	Subjects        []finishSubjectOverride     `json:"subjects"`
}

// finishSubjectOverride replaces a group's SuggestedMsg when the agent
// polishes a low-confidence subject.
type finishSubjectOverride struct {
	GroupID string `json:"group_id"`
	Subject string `json:"subject"`
}

// finishResult is the JSON payload emitted on --json.
type finishResult struct {
	Status            string                    `json:"status"` // "passed" | "failed"
	Commits           []commitflow.CommitRecord `json:"commits"`
	Tag               string                    `json:"tag,omitempty"`
	ReleaseURL        string                    `json:"release_url,omitempty"`
	LastCommitSubject string                    `json:"last_commit_subject,omitempty"`
	CrossRepo         []commitflow.RepoStatus   `json:"cross_repo"`
	FailureDetail     string                    `json:"failure_detail,omitempty"`
}

type commitFinishCommand struct {
	PrepToken string `required:"" help:"Token returned by commit prep (required)"`
	Decisions string `help:"LLM decisions JSON string or @path"`
	Push      bool   `help:"git push origin <branch> --follow-tags after commits"`
	Tag       string `help:"Create annotated tag at HEAD (vX.Y.Z)"`
	JSON      bool   `default:"true" help:"Emit machine-readable JSON result on stdout"`
}

func (c *commitFinishCommand) Run(ctx context.Context, ioctx *commandIO) error {
	return runCommitFinish(ctx, ioctx.stdout, ioctx.stderr, c.PrepToken, c.Decisions, c.Push, c.Tag, c.JSON)
}

func runCommitFinish(ctx context.Context, stdout, stderr io.Writer, prepToken, decisionsArg string, push bool, tag string, jsonOut bool) error {
	// Step 1: load prep state.
	state, err := commitflow.LoadPrepState(prepToken)
	if err != nil {
		return finishFatal(stdout, jsonOut, 2, fmt.Sprintf("load prep state: %v", err))
	}

	if err := commitflow.VerifyPrepStateFresh(ctx, state); err != nil {
		return finishFatal(stdout, jsonOut, 2, fmt.Sprintf("stale prep state: %v", err))
	}

	repoRoot := state.RepoRoot

	// Step 2: parse and apply decisions.
	groups := state.Plan
	dec := &finishDecisions{}
	if decisionsArg != "" {
		parsed, parseErr := parseDecisions(decisionsArg)
		if parseErr != nil {
			return finishFatal(stdout, jsonOut, 2, fmt.Sprintf("parse decisions: %v", parseErr))
		}
		dec = parsed
	}

	if err := validateAndApplyReviewDecisions(ctx, repoRoot, state, dec.ReviewDecisions); err != nil {
		return finishFatal(stdout, jsonOut, 2, err.Error())
	}

	if decisionsArg != "" {
		// Apply review decisions (secret/PII substitutions).
		// Override group subjects from LLM polishing.
		if len(dec.Subjects) > 0 {
			subjectMap := make(map[string]string, len(dec.Subjects))
			for _, s := range dec.Subjects {
				subjectMap[s.GroupID] = s.Subject
			}
			for i := range groups {
				if subj, ok := subjectMap[groups[i].ID]; ok {
					groups[i].SuggestedMsg = subj
				}
			}
		}
	}

	// Review decisions legitimately mutate planned files; refresh the binding
	// so a retry of the same token still passes freshness verification.
	if len(dec.ReviewDecisions) > 0 {
		if headSHA, fileHashes, bindErr := commitflow.BuildPrepStateBinding(ctx, repoRoot, state.Plan); bindErr == nil {
			state.HeadSHA = headSHA
			state.FileHashes = fileHashes
			_ = commitflow.PersistPrepStateAt(prepToken, state)
		}
	}

	// Step 3: Justfile branch.
	if state.ReleaseRecipe && tag != "" {
		bump := bumpLevel(groups, tag)
		if justErr := runJustRelease(ctx, stderr, repoRoot, bump); justErr != nil {
			return finishFatal(stdout, jsonOut, 4, fmt.Sprintf("just release: %v", justErr))
		}
		_ = commitflow.DeletePrepState(prepToken)
		return runVerifyAndEmit(ctx, stdout, repoRoot, state.SessionRepos, nil, tag, jsonOut)
	}

	// Step 4: standard execute path.
	execResult, execErr := commitflow.ExecuteFromGroups(ctx, repoRoot, groups, commitflow.ExecuteOptions{
		Push: push,
		Tag:  tag,
	})
	if execErr != nil {
		code := commitflow.ExecExitCode(execErr)
		detail := execErr.Error()
		// For exit-3/4 failures after some commits landed, still emit the partial result.
		if (code == 3 || code == 4) && execResult != nil {
			return emitFinishResult(stdout, jsonOut, code, buildFinishResult(finishStatusFailed, execResult, detail))
		}
		return finishFatal(stdout, jsonOut, code, detail)
	}

	_ = commitflow.DeletePrepState(prepToken)
	return runVerifyAndEmit(ctx, stdout, repoRoot, state.SessionRepos, execResult, tag, jsonOut)
}

func validateAndApplyReviewDecisions(ctx context.Context, repoRoot string, state *commitflow.PrepState, decisions []commitflow.ReviewDecision) error {
	if state.Analysis == nil {
		if len(decisions) > 0 {
			return fmt.Errorf("review decisions: missing analysis state")
		}
		return nil
	}
	if state.Analysis.Secrets.AmbiguousCount == 0 && len(decisions) == 0 {
		return nil
	}
	if state.Analysis.Refs.Findings == "" {
		return fmt.Errorf("review decisions: missing findings artifact")
	}

	findings, err := commitflow.LoadFindings(state.Analysis.Refs.Findings)
	if err != nil {
		return fmt.Errorf("load findings: %w", err)
	}
	if err := commitflow.ValidateReviewDecisions(findings, decisions); err != nil {
		return fmt.Errorf("review decisions: %w", err)
	}
	if len(decisions) == 0 {
		return nil
	}
	if err := commitflow.ApplyReviewDecisions(ctx, repoRoot, decisions, findings); err != nil {
		return fmt.Errorf("apply review decisions: %w", err)
	}
	return nil
}

// runVerifyAndEmit runs cross-repo + self-verify then emits the final JSON.
func runVerifyAndEmit(ctx context.Context, stdout io.Writer, repoRoot string, sessionRepos []string, execResult *commitflow.ExecuteResult, tag string, jsonOut bool) error {
	crossRepo, _ := commitflow.CrossRepoVerify(ctx, sessionRepos)
	selfResult, selfErr := commitflow.SelfVerify(ctx, repoRoot, "auto")

	status := finishStatusPassed
	failureDetail := ""
	if selfErr != nil {
		status = finishStatusFailed
		failureDetail = selfResult.FailureDetail
		if failureDetail == "" {
			failureDetail = selfErr.Error()
		}
	} else if !selfResult.OK {
		status = finishStatusFailed
		failureDetail = selfResult.FailureDetail
	}

	fr := &finishResult{
		Status:            status,
		CrossRepo:         crossRepo,
		Tag:               tag,
		FailureDetail:     failureDetail,
		LastCommitSubject: selfResult.LastCommitSubject,
	}
	if selfResult.ReleaseURL != "" {
		fr.ReleaseURL = selfResult.ReleaseURL
	}
	if execResult != nil {
		fr.Commits = execResult.Commits
		if execResult.ReleaseURL != nil {
			fr.ReleaseURL = *execResult.ReleaseURL
		}
		if execResult.Tag != nil && fr.Tag == "" {
			fr.Tag = *execResult.Tag
		}
	}

	exitCode := 0
	if status == finishStatusFailed {
		exitCode = 5
	}
	return emitFinishResult(stdout, jsonOut, exitCode, fr)
}
