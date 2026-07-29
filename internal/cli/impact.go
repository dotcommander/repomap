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
)

type impactCommand struct {
	JSON     bool   `help:"Emit machine-readable impact JSON"`
	Markdown bool   `help:"Emit compact Markdown impact handoff"`
	File     string `arg:"" type:"path" help:"File to inspect"`
}

func (c *impactCommand) Run(ctx context.Context, ioctx *commandIO) error {
	if c.JSON && c.Markdown {
		return fmt.Errorf("--json and --markdown are mutually exclusive")
	}
	root, rel, err := impactRootAndPath(ctx, c.File)
	if err != nil {
		return err
	}
	m := repomap.New(root, repomap.DefaultConfig())
	if err := m.Build(ctx); err != nil {
		return err
	}
	impact, err := m.Impact(rel)
	if err != nil {
		return err
	}
	if c.JSON {
		enc := json.NewEncoder(ioctx.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(impact)
	}
	if c.Markdown {
		return printImpactMarkdown(ioctx.stdout, impact)
	}
	return printImpact(ioctx.stdout, impact)
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

func printImpact(w io.Writer, impact repomap.ImpactResult) error {
	if _, err := fmt.Fprintf(w, "%s\n", impact.File.Path); err != nil {
		return err
	}
	if impact.ParseMethod != "" {
		if _, err := fmt.Fprintf(w, "  parsed: %s\n", impact.ParseMethod); err != nil {
			return err
		}
	}
	if len(impact.Boundaries) > 0 {
		if _, err := fmt.Fprintf(w, "  boundaries: %s\n", strings.Join(impact.Boundaries, ", ")); err != nil {
			return err
		}
	}
	if len(impact.Imports) > 0 {
		if _, err := fmt.Fprintf(w, "  imports: %s\n", strings.Join(impact.Imports, ", ")); err != nil {
			return err
		}
	}
	if len(impact.ImportedBy) > 0 {
		if _, err := fmt.Fprintf(w, "  imported by: %s\n", strings.Join(impact.ImportedBy, ", ")); err != nil {
			return err
		}
	}
	if len(impact.Tests) > 0 {
		if _, err := fmt.Fprintf(w, "  tests: %s\n", strings.Join(impact.Tests, ", ")); err != nil {
			return err
		}
	}
	if len(impact.ExportedSymbols) > 0 {
		names := make([]string, 0, len(impact.ExportedSymbols))
		for _, s := range impact.ExportedSymbols {
			names = append(names, s.Name)
		}
		sort.Strings(names)
		if _, err := fmt.Fprintf(w, "  exported: %s\n", strings.Join(names, ", ")); err != nil {
			return err
		}
	}
	if len(impact.ScoreComponents) > 0 {
		if _, err := fmt.Fprintf(w, "  score: %d %v\n", impact.File.Score, impact.ScoreComponents); err != nil {
			return err
		}
	}
	if impact.RiskLevel != "" {
		if _, err := fmt.Fprintf(w, "  risk: %s\n", impact.RiskLevel); err != nil {
			return err
		}
	}
	if len(impact.AffectedPackages) > 0 {
		if _, err := fmt.Fprintf(w, "  affected packages: %s\n", strings.Join(impact.AffectedPackages, ", ")); err != nil {
			return err
		}
	}
	if len(impact.CheckNext) > 0 {
		if _, err := fmt.Fprintf(w, "  check next: %s\n", strings.Join(impact.CheckNext, "; ")); err != nil {
			return err
		}
	}
	if len(impact.LikelyTestCommands) > 0 {
		if _, err := fmt.Fprintf(w, "  likely test commands: %s\n", strings.Join(impact.LikelyTestCommands, "; ")); err != nil {
			return err
		}
	}
	if len(impact.ReadNext) > 0 {
		if _, err := fmt.Fprintln(w, "  read next:"); err != nil {
			return err
		}
		for _, item := range impact.ReadNext {
			if _, err := fmt.Fprintf(w, "    - %s:%d-%d %s\n", item.File, item.StartLine, item.EndLine, item.Reason); err != nil {
				return err
			}
		}
	}
	return nil
}

func printImpactMarkdown(w io.Writer, impact repomap.ImpactResult) error {
	if _, err := fmt.Fprintf(w, "# Impact: `%s`\n\n", impact.File.Path); err != nil {
		return err
	}
	if err := printMarkdownField(w, "Risk", impact.RiskLevel); err != nil {
		return err
	}
	if impact.ParseMethod != "" {
		if err := printMarkdownField(w, "Parsed", impact.ParseMethod); err != nil {
			return err
		}
	}
	if err := printMarkdownField(w, "Score", fmt.Sprintf("%d", impact.File.Score)); err != nil {
		return err
	}
	for _, list := range []struct {
		title  string
		values []string
	}{
		{"Boundaries", impact.Boundaries},
		{"Affected Packages", impact.AffectedPackages},
		{"Imports", impact.Imports},
		{"Imported By", impact.ImportedBy},
		{"Tests", impact.Tests},
	} {
		if err := printMarkdownList(w, list.title, list.values); err != nil {
			return err
		}
	}
	if err := printMarkdownSymbols(w, impact.ExportedSymbols); err != nil {
		return err
	}
	if err := printMarkdownScoreComponents(w, impact.ScoreComponents); err != nil {
		return err
	}
	for _, list := range []struct {
		title  string
		values []string
	}{
		{"Check Next", impact.CheckNext},
		{"Likely Test Commands", impact.LikelyTestCommands},
	} {
		if err := printMarkdownList(w, list.title, list.values); err != nil {
			return err
		}
	}
	if err := printMarkdownReadNext(w, impact.ReadNext); err != nil {
		return err
	}
	if impact.OmittedReason != "" {
		return printMarkdownField(w, "Omitted", impact.OmittedReason)
	}
	return nil
}

func printMarkdownField(w io.Writer, label, value string) error {
	if value == "" {
		return nil
	}
	_, err := fmt.Fprintf(w, "- **%s:** %s\n", label, value)
	return err
}

func printMarkdownList(w io.Writer, title string, values []string) error {
	if len(values) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\n## %s\n\n", title); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(w, "- `%s`\n", value); err != nil {
			return err
		}
	}
	return nil
}

func printMarkdownSymbols(w io.Writer, symbols []repomap.Symbol) error {
	if len(symbols) == 0 {
		return nil
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
	return printMarkdownList(w, "Exported Symbols", names)
}

func printMarkdownScoreComponents(w io.Writer, scores map[string]int) error {
	if len(scores) == 0 {
		return nil
	}
	keys := make([]string, 0, len(scores))
	for key := range scores {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if _, err := fmt.Fprint(w, "\n## Score Components\n\n"); err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := fmt.Fprintf(w, "- `%s`: %d\n", key, scores[key]); err != nil {
			return err
		}
	}
	return nil
}

func printMarkdownReadNext(w io.Writer, items []repomap.ReadNextItem) error {
	if len(items) == 0 {
		return nil
	}
	if _, err := fmt.Fprint(w, "\n## Read Next\n\n"); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintf(w, "- `%s:%d-%d` - %s\n", item.File, item.StartLine, item.EndLine, item.Reason); err != nil {
			return err
		}
	}
	return nil
}
