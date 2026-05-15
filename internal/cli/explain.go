package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

func newExplainCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "explain <file>",
		Short: "Show why a file ranked and rendered the way it did",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, rel, err := impactRootAndPath(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			m := repomap.New(root, repomap.DefaultConfig())
			if err := m.Build(context.Background()); err != nil {
				return err
			}
			explain, err := m.Explain(rel)
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(explain)
			}
			printExplain(os.Stdout, explain)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable explain JSON")
	return cmd
}

func printExplain(w io.Writer, explain repomap.ExplainResult) {
	fmt.Fprintf(w, "%s\n", explain.File.Path)
	fmt.Fprintf(w, "  score: %d\n", explain.Score)
	if explain.DetailLevel >= 0 {
		fmt.Fprintf(w, "  detail: %d\n", explain.DetailLevel)
	} else {
		fmt.Fprintf(w, "  detail: omitted")
		if explain.OmittedReason != "" {
			fmt.Fprintf(w, " (%s)", explain.OmittedReason)
		}
		fmt.Fprintln(w)
	}
	if len(explain.ScoreComponents) == 0 {
		return
	}
	keys := make([]string, 0, len(explain.ScoreComponents))
	for k := range explain.ScoreComponents {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintln(w, "  components:")
	for _, k := range keys {
		fmt.Fprintf(w, "    %s: %+d\n", k, explain.ScoreComponents[k])
	}
}
