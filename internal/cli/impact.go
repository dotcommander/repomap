package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

func newImpactCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "impact <file>",
		Short: "Show deterministic local impact facts for a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, rel, err := impactRootAndPath(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			m := repomap.New(root, repomap.DefaultConfig())
			if err := m.Build(cmd.Context()); err != nil {
				return err
			}
			impact, err := m.Impact(rel)
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(impact)
			}
			printImpact(os.Stdout, impact)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable impact JSON")
	return cmd
}

func impactRootAndPath(ctx context.Context, arg string) (root, rel string, err error) {
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", "", fmt.Errorf("resolve file: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", filepath.Dir(abs), "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("find git root: %w", err)
	}
	root = strings.TrimSpace(string(out))
	rel, err = filepath.Rel(root, abs)
	if err != nil {
		return "", "", fmt.Errorf("relativize file: %w", err)
	}
	return root, filepath.ToSlash(rel), nil
}

func printImpact(w io.Writer, impact repomap.ImpactResult) {
	fmt.Fprintf(w, "%s\n", impact.File.Path)
	if impact.ParseMethod != "" {
		fmt.Fprintf(w, "  parsed: %s\n", impact.ParseMethod)
	}
	if len(impact.Boundaries) > 0 {
		fmt.Fprintf(w, "  boundaries: %s\n", strings.Join(impact.Boundaries, ", "))
	}
	if len(impact.Imports) > 0 {
		fmt.Fprintf(w, "  imports: %s\n", strings.Join(impact.Imports, ", "))
	}
	if len(impact.ImportedBy) > 0 {
		fmt.Fprintf(w, "  imported by: %s\n", strings.Join(impact.ImportedBy, ", "))
	}
	if len(impact.Tests) > 0 {
		fmt.Fprintf(w, "  tests: %s\n", strings.Join(impact.Tests, ", "))
	}
	if len(impact.ExportedSymbols) > 0 {
		names := make([]string, 0, len(impact.ExportedSymbols))
		for _, s := range impact.ExportedSymbols {
			names = append(names, s.Name)
		}
		sort.Strings(names)
		fmt.Fprintf(w, "  exported: %s\n", strings.Join(names, ", "))
	}
	if len(impact.ScoreComponents) > 0 {
		fmt.Fprintf(w, "  score: %d %v\n", impact.File.Score, impact.ScoreComponents)
	}
}
