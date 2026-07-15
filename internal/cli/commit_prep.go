package cli

// commit_prep.go — `repomap commit prep` subcommand wiring.
//
// All types and stateless helpers live in repomap.commit_prep_helpers.go.
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
func buildPrepPayload(ctx context.Context, repoRoot string, noReview, withTag, allowLarge bool) (*repomap.PrepPayload, error) {
	// Step 1: analyze.
	analysis, err := repomap.AnalyzeCommit(ctx, repomap.AnalyzeOptions{Root: repoRoot})
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}

	// Early exit: nothing to commit.
	if analysis.EarlyExit {
		preflight := buildPrepPreflight(ctx, repoRoot, analysis)
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
	applied, _, fixErr := repomap.ApplyFixFindings(ctx, repoRoot, findings)
	if fixErr != nil {
		preflight := buildPrepPreflight(ctx, repoRoot, analysis)
		return &repomap.PrepPayload{
			Preflight:       preflight,
			ModeHint:        repomap.ModeHint(preflight),
			PrepToken:       "none",
			Status:          repomap.PrepStatusAbort,
			AbortReason:     fmt.Sprintf("apply fix findings: %v (working tree may be partially redacted)", fixErr),
			Plan:            []repomap.PrepPlanGroup{},
			Review:          []repomap.PrepReviewItem{},
			LowConfSubjects: []repomap.PrepLowConf{},
			SessionRepos:    repomap.DetectSessionRepos(repoRoot),
		}, nil
	}
	if len(applied) > 0 {
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
		gate = repomap.RunReleaseGateContext(ctx, repoRoot)
	}

	// Step 7: detect Justfile release recipe.
	hasRecipe := repomap.DetectJustfileRelease(repoRoot)

	// Step 8: stash artifacts.
	repomap.StashArtifactsContext(ctx, repoRoot, analysis.Artifacts)

	// Build review items (REVIEW findings, capped at 5).
	reviewItems := repomap.BuildReviewItems(findings, 5)
	reviewCount := repomap.ReviewFindingCount(findings)

	// Status determination.
	status, abortReason := prepStatus(analysis, reviewCount, lowConf)

	// Bind prep state to current git HEAD and file content so finish can
	// detect mutations after prep.
	headSHA, fileHashes, bindErr := repomap.BuildPrepStateBinding(ctx, repoRoot, groups)
	if bindErr != nil {
		return nil, fmt.Errorf("bind prep state: %w", bindErr)
	}

	// Step 9: persist state.
	state := &repomap.PrepState{
		Analysis:      analysis,
		Plan:          groups,
		SessionRepos:  repomap.DetectSessionRepos(repoRoot),
		ReleaseRecipe: hasRecipe,
		ReleaseGate:   gate,
		RepoRoot:      repoRoot,
		HeadSHA:       headSHA,
		FileHashes:    fileHashes,
	}
	token, err := repomap.PersistPrepState(state)
	if err != nil {
		return nil, fmt.Errorf("persist prep state: %w", err)
	}

	// Step 10: assemble payload.
	preflight := buildPrepPreflight(ctx, repoRoot, analysis)
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
func prepStatus(a *repomap.CommitAnalysis, reviewCount int, lc []repomap.PrepLowConf) (string, string) {
	switch {
	case reviewCount > 5 || len(lc) > 3:
		return repomap.PrepStatusAbort, "too many ambiguous items, run /dc:commit interactively"
	case a.Secrets.AmbiguousCount > 0 || len(lc) > 0:
		return repomap.PrepStatusNeedsJudgment, ""
	default:
		return repomap.PrepStatusReady, ""
	}
}

// buildPrepPreflight runs the six git/gh probes synchronously.
func buildPrepPreflight(ctx context.Context, repoRoot string, a *repomap.CommitAnalysis) repomap.PrepPreflight {
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
	return repomap.PrepPreflight{
		Branch:    branch,
		Working:   working,
		Remote:    remote,
		Unpushed:  unpushed,
		LatestTag: latestTag,
		GHAuth:    ghAuthLine(ctx),
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
