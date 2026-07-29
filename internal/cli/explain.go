package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/dotcommander/repomap"
)

type explainCommand struct {
	File string `arg:"" type:"path" help:"File to explain"`
	JSON bool   `help:"Emit machine-readable explain JSON"`
}

func (c *explainCommand) Run(ctx context.Context, ioctx *commandIO) error {
	root, rel, err := impactRootAndPath(ctx, c.File)
	if err != nil {
		return err
	}
	m := repomap.New(root, repomap.DefaultConfig())
	if err := m.Build(ctx); err != nil {
		return err
	}
	explain, err := m.Explain(rel)
	if err != nil {
		return err
	}
	if c.JSON {
		enc := json.NewEncoder(ioctx.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(explain)
	}
	return printExplain(ioctx.stdout, explain)
}

// tierAnnotation returns the short clarifying suffix for a tier label.
var tierAnnotation = map[string]string{
	"confirmed":  " (gopls-verified)",
	"lexical":    " (by-name, may be coincidental)",
	"contextual": " (query-dependent)",
}

func printExplain(w io.Writer, explain repomap.ExplainResult) error {
	if _, err := fmt.Fprintf(w, "%s\n", explain.File.Path); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  score: %d\n", explain.Score); err != nil {
		return err
	}
	if explain.DetailLevel >= 0 {
		if _, err := fmt.Fprintf(w, "  detail: %d\n", explain.DetailLevel); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "  detail: omitted"); err != nil {
			return err
		}
		if explain.OmittedReason != "" {
			if _, err := fmt.Fprintf(w, " (%s)", explain.OmittedReason); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if explain.ParseMethod != "" {
		marker := ""
		if explain.ParseMethod == "regex" || explain.ParseMethod == "ctags" {
			marker = " ⚠ low-fidelity symbols"
		}
		if _, err := fmt.Fprintf(w, "  parsed: %s (%s-confidence)%s\n", explain.ParseMethod, explain.ParseConfidence, marker); err != nil {
			return err
		}
	}
	if len(explain.ScoreComponents) == 0 {
		return nil
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
		if _, err := fmt.Fprintf(w, "  %s%s\n", tier, annotation); err != nil {
			return err
		}
		for _, k := range keys {
			if _, err := fmt.Fprintf(w, "    %-12s %+d\n", k, explain.ScoreComponents[k]); err != nil {
				return err
			}
		}
	}
	return nil
}
