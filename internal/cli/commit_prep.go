package cli

// commit_prep.go — `repomap commit prep` subcommand (thin Cobra wiring).
//
// All types and stateless helpers live in repomap.commit_prep_helpers.go.
// This file owns: flag parsing, the 10-step pipeline orchestration, and
// JSON emission to stdout.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

func newCommitPrepCmd() *cobra.Command {
	var (
		jsonOut    bool
		noReview   bool
		tag        bool
		allowLarge bool
	)
	cmd := &cobra.Command{
		Use:   "prep [directory]",
		Short: "Prepare a commit plan (analyze + fix + consolidate) in one call",
		Long: `Runs the full pre-commit pipeline and emits a JSON payload for the agent.

The agent calls 'commit finish --prep-token <t>' with any LLM decisions for
ambiguous findings or low-confidence subjects.

Exit codes:
  0  payload emitted (check status field for "ready"/"needs_judgment"/"abort")
  1  fatal error (I/O, git not a repo, etc.)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) > 0 {
				root = args[0]
			}
			abs, err := filepath.Abs(root)
			if err != nil {
				return fmt.Errorf("resolve root: %w", err)
			}
			return runCommitPrep(cmd.Context(), abs, jsonOut, noReview, tag, allowLarge)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON payload on stdout")
	cmd.Flags().BoolVar(&noReview, "no-review", false, "Skip simplify scan (phase 0.5)")
	cmd.Flags().BoolVar(&tag, "tag", false, "Run release gate (dep bump + build verify)")
	cmd.Flags().BoolVar(&allowLarge, "allow-large", false, "Skip the kitchen-sink guard that downgrades large/cross-plugin groups to needs_judgment.")
	return cmd
}

func runCommitPrep(ctx context.Context, repoRoot string, jsonOut, noReview, withTag, allowLarge bool) error {
	payload, err := buildPrepPayload(ctx, repoRoot, noReview, withTag, allowLarge)
	if err != nil {
		return err
	}
	return emitPrep(os.Stdout, jsonOut, payload)
}

// buildPrepPayload runs the full prep pipeline and returns the assembled payload.
// Pure helper — does not write to stdout. Reused by `commit auto`.
func buildPrepPayload(ctx context.Context, repoRoot string, noReview, withTag, allowLarge bool) (*repomap.PrepPayload, error) {
	// Step 1: analyze.
	analysis, err := repomap.AnalyzeCommit(ctx, repomap.AnalyzeOptions{Root: repoRoot})
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}

	// Early exit: nothing to commit.
	if analysis.EarlyExit {
		preflight := buildPrepPreflight(repoRoot, analysis)
		return &repomap.PrepPayload{
			Preflight:       preflight,
			ModeHint:        repomap.ModeHint(preflight),
			PrepToken:       "none",
			Status:          repomap.PrepStatusAbort,
			AbortReason:     analysis.EarlyReason,
			Plan:            []repomap.PrepPlanGroup{},
			Review:          []repomap.PrepReviewItem{},
			LowConfSubjects: []repomap.PrepLowConf{},
			SessionRepos:    repomap.DetectSessionRepos(repoRoot),
		}, nil
	}

	// Step 2: simplify scan (unless --no-review).
	if !noReview {
		if candidates, scanErr := repomap.RunSimplifyDetect(ctx, repoRoot); scanErr == nil {
			if applied, _, _ := repomap.ApplyCandidates(ctx, repoRoot, candidates); len(applied) > 0 {
				analysis, err = repomap.AnalyzeCommit(ctx, repomap.AnalyzeOptions{Root: repoRoot})
				if err != nil {
					return nil, fmt.Errorf("re-analyze after simplify: %w", err)
				}
			}
		}
	}

	// Step 3: apply default_action=fix findings; re-analyze if any applied.
	findings, _ := repomap.LoadFindings(analysis.Refs.Findings)
	if applied, _, _ := repomap.ApplyFixFindings(ctx, repoRoot, findings); len(applied) > 0 {
		analysis, err = repomap.AnalyzeCommit(ctx, repomap.AnalyzeOptions{Root: repoRoot})
		if err != nil {
			return nil, fmt.Errorf("re-analyze after fix findings: %w", err)
		}
		findings, _ = repomap.LoadFindings(analysis.Refs.Findings)
	}

	// Step 4: consolidate groups.
	groups := repomap.ConsolidateGroups(analysis.Groups)

	// Step 5: polish low-confidence subjects; collect groups still needing LLM.
	var lowConf []repomap.PrepLowConf
	for i := range groups {
		if groups[i].Confidence < 0.75 {
			if !repomap.PolishGroup(&groups[i], 0.6) {
				lowConf = append(lowConf, repomap.PrepLowConf{
					GroupID:   groups[i].ID,
					Files:     groups[i].Files,
					DiffSlice: repomap.LoadDiffSlice(analysis.Refs.Diffs, groups[i], 500),
				})
			}
		}
	}

	// Step 5b: kitchen-sink guard — force LLM judgment on groups that look like
	// accidental fusion regardless of edge confidence. Suppressed by --allow-large.
	if !allowLarge {
		for i := range groups {
			if repomap.IsKitchenSink(&groups[i]) {
				groups[i].Confidence = 0
				if !repomap.ContainsLowConf(lowConf, groups[i].ID) {
					lowConf = append(lowConf, repomap.PrepLowConf{
						GroupID:   groups[i].ID,
						Files:     groups[i].Files,
						DiffSlice: repomap.LoadDiffSlice(analysis.Refs.Diffs, groups[i], 500),
					})
				}
			}
		}
	}

	// Step 6: release gate (--tag).
	var gate *repomap.PrepReleaseGate
	if withTag {
		gate = repomap.RunReleaseGate(repoRoot)
	}

	// Step 7: detect Justfile release recipe.
	hasRecipe := repomap.DetectJustfileRelease(repoRoot)

	// Step 8: stash artifacts.
	repomap.StashArtifacts(repoRoot, analysis.Artifacts)

	// Build review items (REVIEW findings, capped at 5).
	reviewItems := repomap.BuildReviewItems(findings, 5)

	// Status determination.
	status, abortReason := prepStatus(analysis, reviewItems, lowConf)

	// Step 9: persist state.
	state := &repomap.PrepState{
		Analysis:      analysis,
		Plan:          groups,
		SessionRepos:  repomap.DetectSessionRepos(repoRoot),
		ReleaseRecipe: hasRecipe,
		ReleaseGate:   gate,
		RepoRoot:      repoRoot,
	}
	token, err := repomap.PersistPrepState(state)
	if err != nil {
		return nil, fmt.Errorf("persist prep state: %w", err)
	}

	// Step 10: assemble payload.
	preflight := buildPrepPreflight(repoRoot, analysis)
	return &repomap.PrepPayload{
		Preflight:       preflight,
		ModeHint:        repomap.ModeHint(preflight),
		PrepToken:       token,
		Status:          status,
		AbortReason:     abortReason,
		Plan:            repomap.GroupsToPlan(groups),
		Review:          reviewItems,
		LowConfSubjects: capSlice(lowConf, 3),
		ReleaseRecipe:   hasRecipe,
		SessionRepos:    state.SessionRepos,
		ReleaseGate:     gate,
	}, nil
}

// prepStatus returns the status string and abort reason for the payload.
func prepStatus(a *repomap.CommitAnalysis, review []repomap.PrepReviewItem, lc []repomap.PrepLowConf) (string, string) {
	switch {
	case len(review) > 5 || len(lc) > 3:
		return repomap.PrepStatusAbort, "too many ambiguous items, run /dc:commit interactively"
	case a.Secrets.AmbiguousCount > 0 || len(lc) > 0:
		return repomap.PrepStatusNeedsJudgment, ""
	default:
		return repomap.PrepStatusReady, ""
	}
}

// buildPrepPreflight runs the six git/gh probes synchronously.
func buildPrepPreflight(repoRoot string, a *repomap.CommitAnalysis) repomap.PrepPreflight {
	branch := runTrimmed("git", "-C", repoRoot, "branch", "--show-current")
	working := runTrimmed("git", "-C", repoRoot, "status", "--short")
	remote := runTrimmed("git", "-C", repoRoot, "remote")
	if remote == "" {
		remote = "(none)"
	} else if idx := strings.IndexByte(remote, '\n'); idx >= 0 {
		remote = remote[:idx]
	}
	unpushed := runTrimmed("git", "-C", repoRoot, "log", "--oneline", "@{u}..HEAD")
	latestTag := a.LatestTag
	if latestTag == "" {
		latestTag = "(none)"
	}
	return repomap.PrepPreflight{
		Branch:    branch,
		Working:   working,
		Remote:    remote,
		Unpushed:  unpushed,
		LatestTag: latestTag,
		GHAuth:    ghAuthLine(),
	}
}

// emitPrep writes the PrepPayload to w as JSON, or prints a terse summary.
func emitPrep(w io.Writer, jsonOut bool, p *repomap.PrepPayload) error {
	if !jsonOut {
		fmt.Fprintf(w, "status: %s\n", p.Status)
		if p.AbortReason != "" {
			fmt.Fprintf(w, "abort_reason: %s\n", p.AbortReason)
		}
		fmt.Fprintf(w, "groups: %d\n", len(p.Plan))
		return nil
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	fmt.Fprintln(w)
	return nil
}

// capSlice returns lc[:max] when len > max.
func capSlice(lc []repomap.PrepLowConf, max int) []repomap.PrepLowConf {
	if len(lc) <= max {
		return lc
	}
	return lc[:max]
}
