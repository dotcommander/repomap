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
	cmd.Flags().Float64Var(&confidence, "confidence", 0.75, "Clustering confidence cutoff (0.0–1.0)")
	return cmd
}
