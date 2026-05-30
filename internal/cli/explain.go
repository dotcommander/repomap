package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"

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
			if err := m.Build(cmd.Context()); err != nil {
				return err
			}
			explain, err := m.Explain(rel)
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(explain)
			}
			printExplain(cmd.OutOrStdout(), explain)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable explain JSON")
	return cmd
}

// tierAnnotation returns the short clarifying suffix for a tier label.
var tierAnnotation = map[string]string{
	"confirmed":  " (gopls-verified)",
	"lexical":    " (by-name, may be coincidental)",
	"contextual": " (query-dependent)",
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
	if explain.ParseMethod != "" {
		marker := ""
		if explain.ParseMethod == "regex" || explain.ParseMethod == "ctags" {
			marker = " ⚠ low-fidelity symbols"
		}
		fmt.Fprintf(w, "  parsed: %s (%s-confidence)%s\n", explain.ParseMethod, explain.ParseConfidence, marker)
	}
	if len(explain.ScoreComponents) == 0 {
		return
	}

	// Group components by tier in canonical order.
	for _, tier := range repomap.ConfidenceOrder() {
		// Collect keys that belong to this tier.
		var keys []string
		for k, t := range explain.ComponentTiers {
			if t == tier {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			continue
		}
		slices.Sort(keys)
		annotation := tierAnnotation[tier]
		fmt.Fprintf(w, "  %s%s\n", tier, annotation)
		for _, k := range keys {
			fmt.Fprintf(w, "    %-12s %+d\n", k, explain.ScoreComponents[k])
		}
	}
}
