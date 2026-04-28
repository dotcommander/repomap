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

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
	if decisionsArg != "" {
		dec, parseErr := parseDecisions(decisionsArg)
		if parseErr != nil {
			return finishFatal(jsonOut, 2, fmt.Sprintf("parse decisions: %v", parseErr))
		}
		// Apply review decisions (secret/PII substitutions).
		if len(dec.ReviewDecisions) > 0 {
			findings, _ := repomap.LoadFindings(state.Analysis.Refs.Findings)
			if applyErr := repomap.ApplyReviewDecisions(ctx, repoRoot, dec.ReviewDecisions, findings); applyErr != nil {
				return finishFatal(jsonOut, 2, fmt.Sprintf("apply review decisions: %v", applyErr))
			}
		}
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
		justErr := runJustRelease(ctx, repoRoot, bump)
		if justErr != nil {
			return finishFatal(jsonOut, 4, fmt.Sprintf("just release: %v", justErr))
		}
		// Verify after just release.
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
		// For exit-4 (push/release failed after commits landed), still emit result.
		if code == 4 && execResult != nil {
			return emitFinishResult(jsonOut, code, buildFinishResult("failed", execResult, detail))
		}
		return finishFatal(jsonOut, code, detail)
	}

	return runVerifyAndEmit(ctx, repoRoot, state.SessionRepos, execResult, tag, jsonOut)
}

// runVerifyAndEmit runs cross-repo + self-verify then emits the final JSON.
func runVerifyAndEmit(ctx context.Context, repoRoot string, sessionRepos []string, execResult *repomap.ExecuteResult, tag string, jsonOut bool) error {
	// Step 5: cross-repo verify.
	crossRepo, _ := repomap.CrossRepoVerify(ctx, sessionRepos)

	// Step 6: self-verify.
	selfResult, _ := repomap.SelfVerify(ctx, repoRoot, "auto")

	status := "passed"
	failureDetail := ""
	if !selfResult.OK {
		status = "failed"
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
	if status == "failed" {
		exitCode = 5
	}
	return emitFinishResult(jsonOut, exitCode, fr)
}

// buildFinishResult converts an ExecuteResult to a finishResult for exit-4 partial failures.
func buildFinishResult(status string, r *repomap.ExecuteResult, detail string) *finishResult {
	fr := &finishResult{
		Status:        status,
		Commits:       r.Commits,
		FailureDetail: detail,
	}
	if r.Tag != nil {
		fr.Tag = *r.Tag
	}
	if r.ReleaseURL != nil {
		fr.ReleaseURL = *r.ReleaseURL
	}
	return fr
}

// emitFinishResult writes JSON to stdout (or a terse message) then exits with code.
func emitFinishResult(jsonOut bool, exitCode int, fr *finishResult) error {
	if jsonOut {
		data, err := json.MarshalIndent(fr, "", "  ")
		if err != nil {
			return fmt.Errorf("encode result: %w", err)
		}
		if _, err := os.Stdout.Write(data); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout)
	} else {
		fmt.Printf("status: %s\n", fr.Status)
		if fr.FailureDetail != "" {
			fmt.Printf("failure: %s\n", fr.FailureDetail)
		}
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

// finishFatal emits a minimal JSON error payload and exits.
func finishFatal(jsonOut bool, exitCode int, detail string) error {
	return emitFinishResult(jsonOut, exitCode, &finishResult{
		Status:        "failed",
		FailureDetail: detail,
	})
}

// parseDecisions reads decisions from an inline JSON string or @path file.
func parseDecisions(arg string) (*finishDecisions, error) {
	var raw []byte
	if strings.HasPrefix(arg, "@") {
		data, err := os.ReadFile(arg[1:])
		if err != nil {
			return nil, fmt.Errorf("read decisions file: %w", err)
		}
		raw = data
	} else {
		raw = []byte(arg)
	}
	var d finishDecisions
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// bumpLevel derives the semver bump level from group metadata.
// If tag looks like vX.Y.Z it is returned verbatim (caller passes it to just release).
// Otherwise: any group with Breaking → "major"; any feat → "minor"; else "patch".
func bumpLevel(groups []repomap.CommitGroup, tag string) string {
	// Explicit semver tag: pass through verbatim.
	if strings.HasPrefix(tag, "v") && strings.Count(tag, ".") == 2 {
		return tag
	}
	hasMinor := false
	for _, g := range groups {
		if g.Breaking {
			return "major"
		}
		if g.Type == "feat" {
			hasMinor = true
		}
	}
	if hasMinor {
		return "minor"
	}
	return "patch"
}

// runJustRelease executes `just release <arg>` streaming output to stderr.
func runJustRelease(ctx context.Context, repoRoot, arg string) error {
	cmd := exec.CommandContext(ctx, "just", "release", arg)
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stderr // stream to stderr; stdout reserved for JSON
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
