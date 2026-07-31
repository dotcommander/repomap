package cli

// commit_prep.go — `repomap commit prep` subcommand wiring.
//
// Workflow types and stateless helpers live in internal/commit.
// This file owns: flag parsing, the 10-step pipeline orchestration, and
// JSON emission to stdout.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/dotcommander/repomap"
	commitflow "github.com/dotcommander/repomap/internal/commit"
)

type commitPrepCommand struct {
	Directory  string `arg:"" optional:"" default:"." type:"path" help:"Repository directory"`
	JSON       bool   `help:"Emit machine-readable JSON payload on stdout"`
	NoReview   bool   `help:"Skip simplify scan (phase 0.5)"`
	Tag        bool   `help:"Run release gate (dep bump + build verify)"`
	AllowLarge bool   `help:"Skip the kitchen-sink guard that downgrades large/cross-plugin groups to needs_judgment"`
}

func (c *commitPrepCommand) Run(ctx context.Context, ioctx *commandIO) error {
	abs, err := filepath.Abs(c.Directory)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	return runCommitPrep(ctx, ioctx.stdout, abs, c.JSON, c.NoReview, c.Tag, c.AllowLarge)
}

func runCommitPrep(ctx context.Context, w io.Writer, repoRoot string, jsonOut, noReview, withTag, allowLarge bool) error {
	payload, err := buildPrepPayload(ctx, repoRoot, noReview, withTag, allowLarge)
	if err != nil {
		return err
	}
	return emitPrep(w, jsonOut, payload)
}

// buildPrepPayload runs the full prep pipeline and returns the assembled payload.
// Pure helper — does not write to stdout. Reused by `commit auto`.
func buildPrepPayload(ctx context.Context, repoRoot string, noReview, withTag, allowLarge bool) (*commitflow.PrepPayload, error) {
	// Step 1: analyze.
	analysis, err := repomap.AnalyzeCommit(ctx, repomap.AnalyzeOptions{Root: repoRoot})
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}

	// Early exit: nothing to commit.
	if analysis.EarlyExit {
		preflight := buildPrepPreflight(ctx, repoRoot, analysis)
		return &commitflow.PrepPayload{
			Preflight:       preflight,
			ModeHint:        commitflow.ModeHint(preflight),
			PrepToken:       "none",
			Status:          commitflow.PrepStatusAbort,
			AbortReason:     analysis.EarlyReason,
			Plan:            []commitflow.PrepPlanGroup{},
			Review:          []commitflow.PrepReviewItem{},
			LowConfSubjects: []commitflow.PrepLowConf{},
			SessionRepos:    commitflow.DetectSessionRepos(repoRoot),
		}, nil
	}

	// Step 2: simplify scan (unless --no-review).
	if !noReview {
		if candidates, scanErr := commitflow.RunSimplifyDetect(ctx, repoRoot); scanErr == nil {
			if applied, _, _ := commitflow.ApplyCandidates(ctx, repoRoot, candidates); len(applied) > 0 {
				analysis, err = repomap.AnalyzeCommit(ctx, repomap.AnalyzeOptions{Root: repoRoot})
				if err != nil {
					return nil, fmt.Errorf("re-analyze after simplify: %w", err)
				}
			}
		}
	}

	// Step 3: apply default_action=fix findings; re-analyze if any applied.
	findings, _ := commitflow.LoadFindings(analysis.Refs.Findings)
	applied, _, fixErr := commitflow.ApplyFixFindings(ctx, repoRoot, findings)
	if fixErr != nil {
		preflight := buildPrepPreflight(ctx, repoRoot, analysis)
		return &commitflow.PrepPayload{
			Preflight:       preflight,
			ModeHint:        commitflow.ModeHint(preflight),
			PrepToken:       "none",
			Status:          commitflow.PrepStatusAbort,
			AbortReason:     fmt.Sprintf("apply fix findings: %v (working tree may be partially redacted)", fixErr),
			Plan:            []commitflow.PrepPlanGroup{},
			Review:          []commitflow.PrepReviewItem{},
			LowConfSubjects: []commitflow.PrepLowConf{},
			SessionRepos:    commitflow.DetectSessionRepos(repoRoot),
		}, nil
	}
	if len(applied) > 0 {
		analysis, err = repomap.AnalyzeCommit(ctx, repomap.AnalyzeOptions{Root: repoRoot})
		if err != nil {
			return nil, fmt.Errorf("re-analyze after fix findings: %w", err)
		}
		findings, _ = commitflow.LoadFindings(analysis.Refs.Findings)
	}

	// Step 4: consolidate groups.
	groups := commitflow.ConsolidateGroups(analysis.Groups)

	// Step 5: polish low-confidence subjects; collect groups still needing LLM.
	var lowConf []commitflow.PrepLowConf
	for i := range groups {
		if groups[i].Confidence < 0.75 {
			if !commitflow.PolishGroup(&groups[i], 0.6) {
				lowConf = append(lowConf, commitflow.PrepLowConf{
					GroupID:   groups[i].ID,
					Files:     groups[i].Files,
					DiffSlice: commitflow.LoadDiffSlice(analysis.Refs.Diffs, groups[i], 500),
				})
			}
		}
	}

	// Step 5b: kitchen-sink guard — force LLM judgment on groups that look like
	// accidental fusion regardless of edge confidence. Suppressed by --allow-large.
	if !allowLarge {
		for i := range groups {
			if commitflow.IsKitchenSink(&groups[i]) {
				groups[i].Confidence = 0
				if !commitflow.ContainsLowConf(lowConf, groups[i].ID) {
					lowConf = append(lowConf, commitflow.PrepLowConf{
						GroupID:   groups[i].ID,
						Files:     groups[i].Files,
						DiffSlice: commitflow.LoadDiffSlice(analysis.Refs.Diffs, groups[i], 500),
					})
				}
			}
		}
	}

	// Step 6: release gate (--tag).
	var gate *commitflow.PrepReleaseGate
	if withTag {
		gate = commitflow.RunReleaseGateContext(ctx, repoRoot)
	}

	// Step 7: detect Justfile release recipe.
	hasRecipe := commitflow.DetectJustfileRelease(repoRoot)

	// Step 8: stash artifacts.
	commitflow.StashArtifactsContext(ctx, repoRoot, analysis.Artifacts)

	// Build review items (REVIEW findings, capped at 5).
	reviewItems := commitflow.BuildReviewItems(findings, 5)
	reviewCount := commitflow.ReviewFindingCount(findings)

	// Status determination.
	status, abortReason := prepStatus(analysis, reviewCount, lowConf)

	// Bind prep state to current git HEAD and file content so finish can
	// detect mutations after prep.
	headSHA, fileHashes, bindErr := commitflow.BuildPrepStateBinding(ctx, repoRoot, groups)
	if bindErr != nil {
		return nil, fmt.Errorf("bind prep state: %w", bindErr)
	}

	// Step 9: persist state.
	state := &commitflow.PrepState{
		Analysis:      analysis,
		Plan:          groups,
		SessionRepos:  commitflow.DetectSessionRepos(repoRoot),
		ReleaseRecipe: hasRecipe,
		ReleaseGate:   gate,
		RepoRoot:      repoRoot,
		HeadSHA:       headSHA,
		FileHashes:    fileHashes,
	}
	token, err := commitflow.PersistPrepState(state)
	if err != nil {
		return nil, fmt.Errorf("persist prep state: %w", err)
	}

	// Step 10: assemble payload.
	preflight := buildPrepPreflight(ctx, repoRoot, analysis)
	return &commitflow.PrepPayload{
		Preflight:       preflight,
		ModeHint:        commitflow.ModeHint(preflight),
		PrepToken:       token,
		Status:          status,
		AbortReason:     abortReason,
		Plan:            commitflow.GroupsToPlan(groups),
		Review:          reviewItems,
		LowConfSubjects: capSlice(lowConf, 3),
		ReleaseRecipe:   hasRecipe,
		SessionRepos:    state.SessionRepos,
		ReleaseGate:     gate,
	}, nil
}

// prepStatus returns the status string and abort reason for the payload.
func prepStatus(a *repomap.CommitAnalysis, reviewCount int, lc []commitflow.PrepLowConf) (string, string) {
	switch {
	case reviewCount > 5 || len(lc) > 3:
		return commitflow.PrepStatusAbort, "too many ambiguous items, run /dc:commit interactively"
	case a.Secrets.AmbiguousCount > 0 || len(lc) > 0:
		return commitflow.PrepStatusNeedsJudgment, ""
	default:
		return commitflow.PrepStatusReady, ""
	}
}

// buildPrepPreflight runs the six git/gh probes synchronously.
func buildPrepPreflight(ctx context.Context, repoRoot string, a *repomap.CommitAnalysis) commitflow.PrepPreflight {
	branch := runTrimmed(ctx, "git", "-C", repoRoot, "branch", "--show-current")
	working := runTrimmed(ctx, "git", "-C", repoRoot, "status", "--short")
	remote := runTrimmed(ctx, "git", "-C", repoRoot, "remote")
	if remote == "" {
		remote = "(none)"
	} else if idx := strings.IndexByte(remote, '\n'); idx >= 0 {
		remote = remote[:idx]
	}
	unpushed := runTrimmed(ctx, "git", "-C", repoRoot, "log", "--oneline", "@{u}..HEAD")
	latestTag := a.LatestTag
	if latestTag == "" {
		latestTag = "(none)"
	}
	return commitflow.PrepPreflight{
		Branch:    branch,
		Working:   working,
		Remote:    remote,
		Unpushed:  unpushed,
		LatestTag: latestTag,
		GHAuth:    ghAuthLine(ctx),
	}
}

// emitPrep writes the PrepPayload to w as JSON, or prints a terse summary.
func emitPrep(w io.Writer, jsonOut bool, p *commitflow.PrepPayload) error {
	if !jsonOut {
		if _, err := fmt.Fprintf(w, "status: %s\n", p.Status); err != nil {
			return err
		}
		if p.AbortReason != "" {
			if _, err := fmt.Fprintf(w, "abort_reason: %s\n", p.AbortReason); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(w, "groups: %d\n", len(p.Plan))
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w)
	return err
}

// capSlice returns lc[:max] when len > max.
func capSlice(lc []commitflow.PrepLowConf, max int) []commitflow.PrepLowConf {
	if len(lc) <= max {
		return lc
	}
	return lc[:max]
}
