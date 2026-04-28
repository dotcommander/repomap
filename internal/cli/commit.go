package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

// newCommitCmd builds the `repomap commit` parent + its `analyze` subcommand.
func newCommitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Commit-flow helpers (analyze changesets, emit group plans)",
	}
	cmd.AddCommand(newCommitAnalyzeCmd())
	cmd.AddCommand(newCommitExecuteCmd())
	cmd.AddCommand(newCommitPrepCmd())
	cmd.AddCommand(newCommitFinishCmd())
	cmd.AddCommand(newCommitAutoCmd())
	return cmd
}

func newCommitAnalyzeCmd() *cobra.Command {
	var (
		tag        bool
		pretty     bool
		root       string
		tmpdir     string
		confidence float64
	)
	cmd := &cobra.Command{
		Use:   "analyze [directory]",
		Short: "Analyze changeset and emit a structured commit plan as JSON",
		Long: `Analyzes the current git changeset and emits a compact JSON plan on stdout.
Large content (diffs, untracked, findings) is written to files referenced by
CommitAnalysis.Refs.*.

Typical usage from a commit agent:

    repomap commit analyze | jq .
    repomap commit analyze --tag    # activate release gate`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				root = args[0]
			}
			if root == "" {
				root = "."
			}
			analysis, err := repomap.AnalyzeCommit(context.Background(), repomap.AnalyzeOptions{
				Root:             root,
				Tag:              tag,
				ConfidenceCutoff: confidence,
				Tmpdir:           tmpdir,
			})
			if err != nil {
				return fmt.Errorf("analyze: %w", err)
			}
			data, err := repomap.EncodeJSON(analysis, pretty)
			if err != nil {
				return fmt.Errorf("encode: %w", err)
			}
			if _, err := os.Stdout.Write(data); err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout)
			return nil
		},
	}
	cmd.Flags().BoolVar(&tag, "tag", false, "Activate release gate (go.mod tidy before commit)")
	cmd.Flags().BoolVar(&pretty, "pretty", false, "Pretty-print JSON (default: compact)")
	cmd.Flags().StringVar(&tmpdir, "tmpdir", "", "Override temp directory (for tests)")
	cmd.Flags().Float64Var(&confidence, "confidence", repomap.DefaultConfidenceCutoff, "Clustering confidence cutoff (0.0–1.0)")
	return cmd
}

func newCommitExecuteCmd() *cobra.Command {
	var (
		planFile         string
		push             bool
		tag              string
		noRelease        bool
		releaseNotesFrom string
		dryRun           bool
		jsonOut          bool
		skipFix          bool
	)
	cmd := &cobra.Command{
		Use:   "execute",
		Short: "Execute a commit plan produced by `commit analyze`",
		Long: `Reads a CommitAnalysis JSON plan file and executes the commits deterministically.

Typical usage:

    repomap commit analyze > /tmp/plan.json
    repomap commit execute --plan-file /tmp/plan.json [--push] [--tag v1.2.3]`,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := repomap.ExecuteCommit(cmd.Context(), repomap.ExecuteOptions{
				PlanFile:         planFile,
				Push:             push,
				Tag:              tag,
				NoRelease:        noRelease,
				ReleaseNotesFrom: releaseNotesFrom,
				DryRun:           dryRun,
				JSON:             jsonOut,
				SkipFix:          skipFix,
			})
			if err != nil {
				// For exit-4 errors, still emit JSON if requested before returning.
				if result != nil && jsonOut {
					if data, encErr := repomap.EncodeExecuteResult(result, false); encErr == nil {
						os.Stdout.Write(data) //nolint:errcheck
						fmt.Fprintln(os.Stdout)
					}
				}
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(repomap.ExecExitCode(err))
			}
			if jsonOut {
				data, encErr := repomap.EncodeExecuteResult(result, false)
				if encErr != nil {
					return fmt.Errorf("encode result: %w", encErr)
				}
				if _, err := os.Stdout.Write(data); err != nil {
					return err
				}
				fmt.Fprintln(os.Stdout)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&planFile, "plan-file", "", "Path to CommitAnalysis JSON (required)")
	cmd.Flags().BoolVar(&push, "push", false, "git push origin <branch> --follow-tags after commits")
	cmd.Flags().StringVar(&tag, "tag", "", "Create annotated tag at HEAD (vX.Y.Z format)")
	cmd.Flags().BoolVar(&noRelease, "no-release", false, "Skip gh release create even with --push --tag")
	cmd.Flags().StringVar(&releaseNotesFrom, "release-notes-from", "", "Pass --notes-start-tag to gh release create")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print intended actions, mutate nothing")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable result on stdout")
	cmd.Flags().BoolVar(&skipFix, "skip-fix", false, "Bypass cap-3/fold-riders/merge-smallest consolidation")
	_ = cmd.MarkFlagRequired("plan-file")
	return cmd
}
