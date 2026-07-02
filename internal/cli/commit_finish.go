package cli

// commit_finish.go — `repomap commit finish` subcommand.
//
// Loads the PrepState written by `commit prep`, applies any LLM decisions,
// then either runs `just release <bump>` (Justfile path) or calls
// repomap.ExecuteFromGroups (standard path), followed by cross-repo and
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

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

// finishDecisions is the JSON schema accepted by --decisions.
type finishDecisions struct {
	ReviewDecisions []repomap.ReviewDecision `json:"review_decisions"`
	Subjects        []finishSubjectOverride  `json:"subjects"`
}

// finishSubjectOverride replaces a group's SuggestedMsg when the agent
// polishes a low-confidence subject.
type finishSubjectOverride struct {
	GroupID string `json:"group_id"`
	Subject string `json:"subject"`
}

// finishResult is the JSON payload emitted on --json.
type finishResult struct {
	Status            string                 `json:"status"` // "passed" | "failed"
	Commits           []repomap.CommitRecord `json:"commits"`
	Tag               string                 `json:"tag,omitempty"`
	ReleaseURL        string                 `json:"release_url,omitempty"`
	LastCommitSubject string                 `json:"last_commit_subject,omitempty"`
	CrossRepo         []repomap.RepoStatus   `json:"cross_repo"`
	FailureDetail     string                 `json:"failure_detail,omitempty"`
}

func newCommitFinishCmd() *cobra.Command {
	var (
		prepToken string
		decisions string
		push      bool
		tag       string
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "finish",
		Short: "Execute a prepared commit plan (output of `commit prep`)",
		Long: `Loads the state written by 'commit prep --prep-token <t>' and executes it.

Accepts optional LLM decisions for ambiguous findings and low-confidence
subjects via --decisions (inline JSON or @path).

Exit codes:
  0  passed
  2  plan/decision validation
  3  git/execute failure
  4  push/release failure (commits already landed)
  5  verify failure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommitFinish(cmd.Context(), prepToken, decisions, push, tag, jsonOut)
		},
	}
	cmd.Flags().StringVar(&prepToken, "prep-token", "", "Token returned by `commit prep` (required)")
	cmd.Flags().StringVar(&decisions, "decisions", "", "LLM decisions JSON string or @path")
	cmd.Flags().BoolVar(&push, "push", false, "git push origin <branch> --follow-tags after commits")
	cmd.Flags().StringVar(&tag, "tag", "", "Create annotated tag at HEAD (vX.Y.Z)")
	cmd.Flags().BoolVar(&jsonOut, "json", true, "Emit machine-readable JSON result on stdout")
	_ = cmd.MarkFlagRequired("prep-token")
	return cmd
}

func runCommitFinish(ctx context.Context, prepToken, decisionsArg string, push bool, tag string, jsonOut bool) error {
	// Step 1: load prep state.
	state, err := repomap.LoadPrepState(prepToken)
	if err != nil {
		return finishFatal(jsonOut, 2, fmt.Sprintf("load prep state: %v", err))
	}
	repoRoot := state.RepoRoot

	// Step 2: parse and apply decisions.
	groups := state.Plan
	dec := &finishDecisions{}
	if decisionsArg != "" {
		parsed, parseErr := parseDecisions(decisionsArg)
		if parseErr != nil {
			return finishFatal(jsonOut, 2, fmt.Sprintf("parse decisions: %v", parseErr))
		}
		dec = parsed
	}

	if err := validateAndApplyReviewDecisions(ctx, repoRoot, state, dec.ReviewDecisions); err != nil {
		return finishFatal(jsonOut, 2, err.Error())
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

	// Step 3: Justfile branch.
	if state.ReleaseRecipe && tag != "" {
		bump := bumpLevel(groups, tag)
		if justErr := runJustRelease(ctx, repoRoot, bump); justErr != nil {
			return finishFatal(jsonOut, 4, fmt.Sprintf("just release: %v", justErr))
		}
		return runVerifyAndEmit(ctx, repoRoot, state.SessionRepos, nil, tag, jsonOut)
	}

	// Step 4: standard execute path.
	execResult, execErr := repomap.ExecuteFromGroups(ctx, repoRoot, groups, repomap.ExecuteOptions{
		Push: push,
		Tag:  tag,
	})
	if execErr != nil {
		code := repomap.ExecExitCode(execErr)
		detail := execErr.Error()
		// For exit-3/4 failures after some commits landed, still emit the partial result.
		if (code == 3 || code == 4) && execResult != nil {
			return emitFinishResult(jsonOut, code, buildFinishResult(finishStatusFailed, execResult, detail))
		}
		return finishFatal(jsonOut, code, detail)
	}

	return runVerifyAndEmit(ctx, repoRoot, state.SessionRepos, execResult, tag, jsonOut)
}

func validateAndApplyReviewDecisions(ctx context.Context, repoRoot string, state *repomap.PrepState, decisions []repomap.ReviewDecision) error {
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

	findings, err := repomap.LoadFindings(state.Analysis.Refs.Findings)
	if err != nil {
		return fmt.Errorf("load findings: %w", err)
	}
	if err := repomap.ValidateReviewDecisions(findings, decisions); err != nil {
		return fmt.Errorf("review decisions: %w", err)
	}
	if len(decisions) == 0 {
		return nil
	}
	if err := repomap.ApplyReviewDecisions(ctx, repoRoot, decisions, findings); err != nil {
		return fmt.Errorf("apply review decisions: %w", err)
	}
	return nil
}

// runVerifyAndEmit runs cross-repo + self-verify then emits the final JSON.
func runVerifyAndEmit(ctx context.Context, repoRoot string, sessionRepos []string, execResult *repomap.ExecuteResult, tag string, jsonOut bool) error {
	crossRepo, _ := repomap.CrossRepoVerify(ctx, sessionRepos)
	selfResult, selfErr := repomap.SelfVerify(ctx, repoRoot, "auto")

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
	return emitFinishResult(jsonOut, exitCode, fr)
}
