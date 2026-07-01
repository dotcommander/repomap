package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

func newImpactCmd() *cobra.Command {
	var (
		jsonOut     bool
		markdownOut bool
	)
	cmd := &cobra.Command{
		Use:   "impact <file>",
		Short: "Show deterministic local impact facts for a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut && markdownOut {
				return fmt.Errorf("--json and --markdown are mutually exclusive")
			}
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
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(impact)
			}
			if markdownOut {
				printImpactMarkdown(cmd.OutOrStdout(), impact)
				return nil
			}
			printImpact(cmd.OutOrStdout(), impact)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable impact JSON")
	cmd.Flags().BoolVar(&markdownOut, "markdown", false, "Emit compact Markdown impact handoff")
	return cmd
}

func impactRootAndPath(ctx context.Context, arg string) (root, rel string, err error) {
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", "", fmt.Errorf("resolve file: %w", err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", fmt.Errorf("resolve file symlinks: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", filepath.Dir(abs), "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("find git root: %w", err)
	}
	root = strings.TrimSpace(string(out))
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve git root symlinks: %w", err)
	}
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
	if impact.RiskLevel != "" {
		fmt.Fprintf(w, "  risk: %s\n", impact.RiskLevel)
	}
	if len(impact.AffectedPackages) > 0 {
		fmt.Fprintf(w, "  affected packages: %s\n", strings.Join(impact.AffectedPackages, ", "))
	}
	if len(impact.CheckNext) > 0 {
		fmt.Fprintf(w, "  check next: %s\n", strings.Join(impact.CheckNext, "; "))
	}
	if len(impact.LikelyTestCommands) > 0 {
		fmt.Fprintf(w, "  likely test commands: %s\n", strings.Join(impact.LikelyTestCommands, "; "))
	}
	if len(impact.ReadNext) > 0 {
		fmt.Fprintln(w, "  read next:")
		for _, item := range impact.ReadNext {
			fmt.Fprintf(w, "    - %s:%d-%d %s\n", item.File, item.StartLine, item.EndLine, item.Reason)
		}
	}
}

func printImpactMarkdown(w io.Writer, impact repomap.ImpactResult) {
	fmt.Fprintf(w, "# Impact: `%s`\n\n", impact.File.Path)
	printMarkdownField(w, "Risk", impact.RiskLevel)
	if impact.ParseMethod != "" {
		printMarkdownField(w, "Parsed", impact.ParseMethod)
	}
	printMarkdownField(w, "Score", fmt.Sprintf("%d", impact.File.Score))
	printMarkdownList(w, "Boundaries", impact.Boundaries)
	printMarkdownList(w, "Affected Packages", impact.AffectedPackages)
	printMarkdownList(w, "Imports", impact.Imports)
	printMarkdownList(w, "Imported By", impact.ImportedBy)
	printMarkdownList(w, "Tests", impact.Tests)
	printMarkdownSymbols(w, impact.ExportedSymbols)
	printMarkdownScoreComponents(w, impact.ScoreComponents)
	printMarkdownList(w, "Check Next", impact.CheckNext)
	printMarkdownList(w, "Likely Test Commands", impact.LikelyTestCommands)
	printMarkdownReadNext(w, impact.ReadNext)
	if impact.OmittedReason != "" {
		printMarkdownField(w, "Omitted", impact.OmittedReason)
	}
}

func printMarkdownField(w io.Writer, label, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(w, "- **%s:** %s\n", label, value)
}

func printMarkdownList(w io.Writer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(w, "\n## %s\n\n", title)
	for _, value := range values {
		fmt.Fprintf(w, "- `%s`\n", value)
	}
}

func printMarkdownSymbols(w io.Writer, symbols []repomap.Symbol) {
	if len(symbols) == 0 {
		return
	}
	names := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		name := symbol.Name
		if symbol.Receiver != "" {
			name = symbol.Receiver + "." + symbol.Name
		}
		if symbol.Kind != "" {
			name += " (" + symbol.Kind + ")"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	printMarkdownList(w, "Exported Symbols", names)
}

func printMarkdownScoreComponents(w io.Writer, scores map[string]int) {
	if len(scores) == 0 {
		return
	}
	keys := make([]string, 0, len(scores))
	for key := range scores {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fmt.Fprint(w, "\n## Score Components\n\n")
	for _, key := range keys {
		fmt.Fprintf(w, "- `%s`: %d\n", key, scores[key])
	}
}

func printMarkdownReadNext(w io.Writer, items []repomap.ReadNextItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprint(w, "\n## Read Next\n\n")
	for _, item := range items {
		fmt.Fprintf(w, "- `%s:%d-%d` - %s\n", item.File, item.StartLine, item.EndLine, item.Reason)
	}
}
