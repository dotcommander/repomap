package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

type inventoryReport struct {
	SchemaVersion int      `json:"schema_version"`
	Root          string   `json:"root"`
	Boundary      string   `json:"boundary"`
	Constructors  []string `json:"constructors,omitempty"`
	Writers       []string `json:"writers,omitempty"`
	Readers       []string `json:"readers,omitempty"`
	Migrations    []string `json:"migrations,omitempty"`
	Tests         []string `json:"tests,omitempty"`
	Docs          []string `json:"docs,omitempty"`
}

func newInventoryCmd() *cobra.Command {
	var (
		boundary string
		jsonOut  bool
	)
	cmd := &cobra.Command{
		Use:   "inventory [directory]",
		Short: "Answer ownership for a boundary such as Postgres",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			root, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}
			report, err := buildInventoryReport(cmd, root, boundary)
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			return printInventory(cmd.OutOrStdout(), report)
		},
	}
	cmd.Flags().StringVar(&boundary, "boundary", "", "Boundary to inventory (for example Postgres)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable inventory JSON")
	return cmd
}

func buildInventoryReport(cmd *cobra.Command, root, boundary string) (inventoryReport, error) {
	if strings.TrimSpace(boundary) == "" {
		return inventoryReport{}, fmt.Errorf("--boundary is required")
	}
	cfg := repomap.Config{MaxTokens: 0, MaxTokensNoCtx: 0, Intent: boundary + " database migrations schema queries"}
	m := repomap.New(root, cfg)
	if err := m.Build(cmd.Context()); err != nil {
		return inventoryReport{}, err
	}

	canonicalBoundary, effectKind := normalizeInventoryBoundary(boundary)
	report := inventoryReport{
		SchemaVersion: 1,
		Root:          root,
		Boundary:      canonicalBoundary,
	}

	for _, file := range m.Ranked() {
		if !rankedFileMatchesBoundary(file, canonicalBoundary, effectKind) {
			continue
		}
		report.Constructors = append(report.Constructors, file.Path)
	}

	effects, err := m.AuditEffects(cmd.Context(), 0)
	if err != nil {
		return inventoryReport{}, err
	}
	effects = filterAuditEffects(effects, effectKind)
	for _, file := range effects.Files {
		if databaseEffectHas(file.Effects, "Query") {
			report.Readers = append(report.Readers, file.Path)
		}
		if databaseEffectHas(file.Effects, "Exec", "Begin", "Commit", "Rollback") || !databaseEffectHas(file.Effects, "Query") {
			report.Writers = append(report.Writers, file.Path)
		}
	}

	owned := dedupeStrings(append(append([]string{}, report.Constructors...), report.Writers...))
	for _, path := range owned {
		impact, err := m.Impact(path)
		if err != nil {
			continue
		}
		report.Tests = append(report.Tests, impact.Tests...)
	}

	report.Constructors = dedupeStrings(report.Constructors)
	report.Writers = dedupeStrings(report.Writers)
	report.Readers = dedupeStrings(report.Readers)
	report.Tests = dedupeStrings(report.Tests)
	report.Migrations = findInventoryPaths(root, canonicalBoundary, true)
	report.Docs = findInventoryPaths(root, canonicalBoundary, false)
	return report, nil
}

func normalizeInventoryBoundary(boundary string) (canonical, effectKind string) {
	switch strings.ToLower(strings.TrimSpace(boundary)) {
	case "postgres", "postgresql", "pgx", "sql", "database", "db":
		return "Postgres", "database"
	default:
		return strings.TrimSpace(boundary), normalizeAuditEffectKind(boundary)
	}
}

func rankedFileMatchesBoundary(file repomap.RankedFile, boundary, effectKind string) bool {
	for _, label := range file.Boundaries {
		if strings.EqualFold(label, boundary) {
			return true
		}
	}
	if effectKind == "database" {
		for _, imp := range file.Imports {
			low := strings.ToLower(imp)
			if strings.Contains(low, "pgx") || strings.Contains(low, "database/sql") || strings.Contains(low, "lib/pq") {
				return true
			}
		}
	}
	return false
}

func databaseEffectHas(effects []repomap.AuditEffect, needles ...string) bool {
	for _, effect := range effects {
		text := effect.Op + " " + effect.Evidence
		for _, needle := range needles {
			if strings.Contains(text, needle) {
				return true
			}
		}
	}
	return false
}

func findInventoryPaths(root, boundary string, migrations bool) []string {
	var out []string
	terms := inventorySearchTerms(boundary, migrations)
	if strings.EqualFold(boundary, "Postgres") {
		terms = append(terms, "postgresql", "pgx")
		if migrations {
			terms = append(terms, "database", "migration", "schema")
		}
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && (d.Name() == ".git" || d.Name() == ".work" || d.Name() == "node_modules" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		low := strings.ToLower(rel)
		if migrations {
			if !(strings.Contains(low, "migration") || strings.Contains(low, "schema") || strings.HasSuffix(low, ".sql")) {
				return nil
			}
			if hasAny(low, terms) {
				out = append(out, rel)
			}
			return nil
		}
		if !(strings.HasSuffix(low, ".md") || strings.HasSuffix(low, ".mdx") || strings.HasSuffix(low, ".txt")) {
			return nil
		}
		if hasAny(low, terms) || fileContainsAny(path, terms) {
			out = append(out, rel)
		}
		return nil
	})
	return dedupeStrings(out)
}

func inventorySearchTerms(boundary string, migrations bool) []string {
	term := strings.ToLower(boundary)
	if term == "" {
		return nil
	}
	if migrations {
		return []string{term}
	}
	return []string{term}
}

func hasAny(text string, terms []string) bool {
	for _, term := range terms {
		if term != "" && strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func fileContainsAny(path string, terms []string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if len(data) > 256*1024 {
		data = data[:256*1024]
	}
	return hasAny(strings.ToLower(string(data)), terms)
}

func printInventory(w io.Writer, report inventoryReport) error {
	if _, err := fmt.Fprintf(w, "inventory: %s\n", report.Boundary); err != nil {
		return err
	}
	printInventoryList(w, "constructors", report.Constructors)
	printInventoryList(w, "writers", report.Writers)
	printInventoryList(w, "readers", report.Readers)
	printInventoryList(w, "migrations", report.Migrations)
	printInventoryList(w, "tests", report.Tests)
	printInventoryList(w, "docs", report.Docs)
	return nil
}

func printInventoryList(w io.Writer, label string, values []string) {
	fmt.Fprintf(w, "%s:\n", label)
	if len(values) == 0 {
		fmt.Fprintln(w, "  (none)")
		return
	}
	for _, value := range values {
		fmt.Fprintf(w, "  - %s\n", value)
	}
}
