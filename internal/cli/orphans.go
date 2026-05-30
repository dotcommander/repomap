package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

func newOrphansCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "orphans [directory]",
		Short: "List exported symbols with zero inbound references (dead-code candidates)",
		Long: `Lists exported symbols that have no inbound references within this repository.

Candidates only — repomap sees one repo. Verify external/library consumers
before deleting. Never treat this list as a verdict.

Requires gopls on PATH (same as 'repomap context --calls').`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}
			m := repomap.New(absDir, repomap.Config{MaxTokens: 8192, MaxTokensNoCtx: 8192})
			if err := m.Build(cmd.Context()); err != nil {
				return err
			}
			report, err := runOrphans(cmd.Context(), m, absDir)
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			printOrphans(cmd.OutOrStdout(), report)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable orphan-candidate JSON")
	return cmd
}

func runOrphans(ctx context.Context, m *repomap.Map, root string) (repomap.OrphanReport, error) {
	q, shutdown, err := repomap.OrphanQuerier(root)
	if err != nil {
		return repomap.OrphanReport{}, err
	}
	defer shutdown(context.WithoutCancel(ctx))
	return m.OrphanCandidates(ctx, q)
}

func printOrphans(w io.Writer, r repomap.OrphanReport) {
	fmt.Fprintf(w, "# %s\n\n", r.Caveat)
	fmt.Fprintf(w, "zero references (incl. tests): %d\n", len(r.ZeroRefs))
	printOrphanBucket(w, r.ZeroRefs)
	fmt.Fprintf(w, "\ntest-only references: %d\n", len(r.TestOnlyRefs))
	printOrphanBucket(w, r.TestOnlyRefs)
}

func printOrphanBucket(w io.Writer, cands []repomap.OrphanCandidate) {
	lines := make([]string, 0, len(cands))
	for _, c := range cands {
		name := c.Name
		if c.Receiver != "" {
			name = fmt.Sprintf("(%s) %s", c.Receiver, c.Name)
		}
		lines = append(lines, fmt.Sprintf("  %s  %s  %s:%d", name, c.Kind, c.File, c.Line))
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}
