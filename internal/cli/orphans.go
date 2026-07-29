package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/dotcommander/repomap"
)

type orphansCommand struct {
	Directory string `arg:"" optional:"" type:"path" default:"." help:"Directory to inspect"`
	JSON      bool   `help:"Emit machine-readable orphan-candidate JSON"`
}

func (c *orphansCommand) Run(ctx context.Context, ioctx *commandIO) error {
	absDir, err := filepath.Abs(c.Directory)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	m := repomap.New(absDir, repomap.Config{MaxTokens: 8192, MaxTokensNoCtx: 8192})
	if err := m.Build(ctx); err != nil {
		return err
	}
	report, err := runOrphans(ctx, m, absDir)
	if err != nil {
		return err
	}
	if c.JSON {
		enc := json.NewEncoder(ioctx.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	return printOrphans(ioctx.stdout, report)
}

func runOrphans(ctx context.Context, m *repomap.Map, root string) (repomap.OrphanReport, error) {
	q, shutdown, err := repomap.OrphanQuerier(root)
	if err != nil {
		return repomap.OrphanReport{}, err
	}
	defer shutdown(context.WithoutCancel(ctx))
	return m.OrphanCandidates(ctx, q)
}

func printOrphans(w io.Writer, r repomap.OrphanReport) error {
	if _, err := fmt.Fprintf(w, "# %s\n\n", r.Caveat); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "zero references (incl. tests): %d\n", len(r.ZeroRefs)); err != nil {
		return err
	}
	if err := printOrphanBucket(w, r.ZeroRefs); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\ntest-only references: %d\n", len(r.TestOnlyRefs)); err != nil {
		return err
	}
	return printOrphanBucket(w, r.TestOnlyRefs)
}

func printOrphanBucket(w io.Writer, cands []repomap.OrphanCandidate) error {
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
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	return nil
}
