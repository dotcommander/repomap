package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Emit deterministic audit prepass facts",
		Long: `Emit deterministic audit prepass facts for deep-review workflows.

Audit commands produce leads and lane packets, not final findings. Promote a
lead only after checking source, docs, runtime behavior, or another
authoritative signal.`,
	}
	cmd.AddCommand(newAuditHygieneCmd())
	cmd.AddCommand(newAuditBriefCmd())
	cmd.AddCommand(newAuditRisksCmd())
	cmd.AddCommand(newAuditSurfaceCmd())
	cmd.AddCommand(newAuditEffectsCmd())
	return cmd
}

func newAuditHygieneCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "hygiene [directory]",
		Short: "Report tracked, untracked, and ignored source-file hygiene",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := auditRoot(args)
			if err != nil {
				return err
			}
			report, err := repomap.AuditHygiene(cmd.Context(), root)
			if err != nil {
				return err
			}
			if jsonOut {
				return encodeAuditJSON(cmd.OutOrStdout(), report)
			}
			return printAuditHygiene(cmd.OutOrStdout(), report)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable audit hygiene JSON")
	return cmd
}

func newAuditBriefCmd() *cobra.Command {
	var (
		jsonOut  bool
		limit    int
		topFiles int
		intent   string
	)
	cmd := &cobra.Command{
		Use:   "brief [directory]",
		Short: "Report risks, surfaces, effects, and first-read queue in one map build",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := buildAuditMap(cmd, args, intent)
			if err != nil {
				return err
			}
			report, err := m.AuditBrief(cmd.Context(), auditLimit(limit, topFiles))
			if err != nil {
				return err
			}
			if jsonOut {
				return encodeAuditJSON(cmd.OutOrStdout(), report)
			}
			return printAuditBrief(cmd.OutOrStdout(), report)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable audit brief JSON")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum risk/surface/effect files to emit (0 = all)")
	cmd.Flags().IntVar(&topFiles, "top-files", 0, "Alias for --limit; maximum files to emit (0 = use --limit)")
	cmd.Flags().StringVarP(&intent, "intent", "i", "", "Optional audit intent used to rerank files before packet generation")
	return cmd
}

func newAuditRisksCmd() *cobra.Command {
	var (
		jsonOut  bool
		limit    int
		topFiles int
		intent   string
	)
	cmd := &cobra.Command{
		Use:   "risks [directory]",
		Short: "Report risk-ranked files and suggested deep-audit lanes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := auditRoot(args)
			if err != nil {
				return err
			}
			cfg := repomap.Config{
				MaxTokens:      0,
				MaxTokensNoCtx: 0,
				Intent:         intent,
			}
			m := repomap.New(root, cfg)
			if err := m.Build(cmd.Context()); err != nil {
				return err
			}
			report := m.AuditRisks(auditLimit(limit, topFiles))
			if jsonOut {
				return encodeAuditJSON(cmd.OutOrStdout(), report)
			}
			return printAuditRisks(cmd.OutOrStdout(), report)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable audit risk JSON")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum file risk packets to emit (0 = all)")
	cmd.Flags().IntVar(&topFiles, "top-files", 0, "Alias for --limit; maximum files to emit (0 = use --limit)")
	cmd.Flags().StringVarP(&intent, "intent", "i", "", "Optional audit intent used to rerank files before risk packet generation")
	return cmd
}

func newAuditSurfaceCmd() *cobra.Command {
	var (
		jsonOut  bool
		limit    int
		topFiles int
		intent   string
	)
	cmd := &cobra.Command{
		Use:   "surface [directory]",
		Short: "Report deterministic command, flag, config, schema, route, and output surfaces",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := buildAuditMap(cmd, args, intent)
			if err != nil {
				return err
			}
			report, err := m.AuditSurface(cmd.Context(), auditLimit(limit, topFiles))
			if err != nil {
				return err
			}
			if jsonOut {
				return encodeAuditJSON(cmd.OutOrStdout(), report)
			}
			return printAuditSurface(cmd.OutOrStdout(), report)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable audit surface JSON")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum files to emit (0 = all)")
	cmd.Flags().IntVar(&topFiles, "top-files", 0, "Alias for --limit; maximum files to emit (0 = use --limit)")
	cmd.Flags().StringVarP(&intent, "intent", "i", "", "Optional audit intent used to rerank files before surface extraction")
	return cmd
}

func newAuditEffectsCmd() *cobra.Command {
	var (
		jsonOut   bool
		limit     int
		topFiles  int
		intent    string
		kind      string
		pathsOnly bool
	)
	cmd := &cobra.Command{
		Use:   "effects [directory]",
		Short: "Report deterministic side-effect and trust-boundary packets",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := buildAuditMap(cmd, args, intent)
			if err != nil {
				return err
			}
			report, err := m.AuditEffects(cmd.Context(), auditLimit(limit, topFiles))
			if err != nil {
				return err
			}
			report = filterAuditEffects(report, kind)
			if pathsOnly {
				return printAuditEffectPaths(cmd.OutOrStdout(), report, jsonOut)
			}
			if jsonOut {
				return encodeAuditJSON(cmd.OutOrStdout(), report)
			}
			return printAuditEffects(cmd.OutOrStdout(), report)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable audit effects JSON")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum files to emit (0 = all)")
	cmd.Flags().IntVar(&topFiles, "top-files", 0, "Alias for --limit; maximum files to emit (0 = use --limit)")
	cmd.Flags().StringVar(&kind, "kind", "", "Filter effects by kind (for example database, subprocess, filesystem-write)")
	cmd.Flags().BoolVar(&pathsOnly, "paths-only", false, "Emit only matching file paths")
	cmd.Flags().StringVarP(&intent, "intent", "i", "", "Optional audit intent used to rerank files before effect extraction")
	return cmd
}

func auditLimit(limit, topFiles int) int {
	if topFiles != 0 {
		return topFiles
	}
	return limit
}

func filterAuditEffects(report repomap.AuditEffectReport, kind string) repomap.AuditEffectReport {
	kind = normalizeAuditEffectKind(kind)
	if kind == "" {
		return report
	}
	files := make([]repomap.AuditEffectFile, 0, len(report.Files))
	kindFiles := map[string][]string{}
	for _, file := range report.Files {
		effects := make([]repomap.AuditEffect, 0, len(file.Effects))
		for _, effect := range file.Effects {
			if effect.Kind != kind {
				continue
			}
			effects = append(effects, effect)
			kindFiles[effect.Kind] = append(kindFiles[effect.Kind], file.Path)
		}
		if len(effects) == 0 {
			continue
		}
		file.Effects = effects
		file.Lanes = auditEffectLanes(effects)
		files = append(files, file)
	}
	existingKinds := report.Kinds
	report.Files = files
	report.Kinds = nil
	for _, existing := range buildFilteredEffectKinds(existingKinds, kindFiles) {
		report.Kinds = append(report.Kinds, existing)
	}
	if len(report.Files) == 0 {
		report.FilesOmittedReason = "no side-effect data matched --kind"
	} else {
		report.FilesOmittedReason = ""
	}
	return report
}

func normalizeAuditEffectKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "all":
		return ""
	case "db", "postgres", "postgresql", "pgx", "sql":
		return "database"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func auditEffectLanes(effects []repomap.AuditEffect) []string {
	seen := map[string]bool{}
	var out []string
	for _, effect := range effects {
		if seen[effect.Lane] {
			continue
		}
		seen[effect.Lane] = true
		out = append(out, effect.Lane)
	}
	sort.Strings(out)
	return out
}

func buildFilteredEffectKinds(existing []repomap.AuditEffectKind, kindFiles map[string][]string) []repomap.AuditEffectKind {
	byName := map[string]repomap.AuditEffectKind{}
	for _, kind := range existing {
		byName[kind.Name] = kind
	}
	names := make([]string, 0, len(kindFiles))
	for name := range kindFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]repomap.AuditEffectKind, 0, len(names))
	for _, name := range names {
		kind := byName[name]
		if kind.Name == "" {
			kind = repomap.AuditEffectKind{
				ID:      "repomap:effect-kind:" + strings.ReplaceAll(name, "_", "-"),
				Name:    name,
				Reason:  "filtered static side-effect signal",
				Lane:    auditEffectLanesForKind(name),
				Command: "repomap audit effects --json",
			}
		}
		kind.Files = dedupeStrings(kindFiles[name])
		out = append(out, kind)
	}
	return out
}

func auditEffectLanesForKind(kind string) string {
	switch kind {
	case "database", "filesystem-write", "filesystem-read", "time":
		return "data-integrity"
	case "subprocess", "process-exit":
		return "error-handling"
	case "http", "serialization":
		return "api-contracts"
	case "secret", "crypto", "randomness":
		return "security"
	case "context-background", "goroutine":
		return "lifecycle-concurrency"
	case "unbounded-read":
		return "performance"
	default:
		return "best-practices"
	}
}

func printAuditEffectPaths(w io.Writer, report repomap.AuditEffectReport, jsonOut bool) error {
	paths := make([]string, 0, len(report.Files))
	for _, file := range report.Files {
		paths = append(paths, file.Path)
	}
	paths = dedupeStrings(paths)
	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(paths)
	}
	for _, path := range paths {
		if _, err := fmt.Fprintln(w, path); err != nil {
			return err
		}
	}
	return nil
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func buildAuditMap(cmd *cobra.Command, args []string, intent string) (*repomap.Map, error) {
	root, err := auditRoot(args)
	if err != nil {
		return nil, err
	}
	cfg := repomap.Config{
		MaxTokens:      0,
		MaxTokensNoCtx: 0,
		Intent:         intent,
	}
	m := repomap.New(root, cfg)
	if err := m.Build(cmd.Context()); err != nil {
		return nil, err
	}
	return m, nil
}

func auditRoot(args []string) (string, error) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	return root, nil
}

func encodeAuditJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func printAuditHygiene(w io.Writer, report repomap.AuditHygieneReport) error {
	if _, err := fmt.Fprintf(w, "audit hygiene: %s\n", report.Root); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  tracked source: %d\n", report.Counts.TrackedSource); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  untracked source: %d\n", report.Counts.UntrackedCode); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  ignored source: %d\n", report.Counts.IgnoredSource); err != nil {
		return err
	}
	if report.Counts.SuppressedUntrackedCode > 0 || report.Counts.SuppressedIgnoredSource > 0 {
		if _, err := fmt.Fprintf(w, "  suppressed noise: untracked=%d ignored=%d\n",
			report.Counts.SuppressedUntrackedCode, report.Counts.SuppressedIgnoredSource); err != nil {
			return err
		}
	}
	for _, issue := range report.Issues {
		if _, err := fmt.Fprintf(w, "  [%s] %s %s: %s\n", issue.Severity, issue.ID, issue.Path, issue.Evidence); err != nil {
			return err
		}
	}
	return nil
}

func printAuditBrief(w io.Writer, report repomap.AuditBriefReport) error {
	if _, err := fmt.Fprintf(w, "audit brief: %s\n", report.Root); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  risks=%d surface_files=%d effect_files=%d first_read_groups=%d review_plan_lanes=%d\n",
		len(report.Risks.Files), len(report.Surface.Files), len(report.Effects.Files), len(report.FirstReadQueue), len(report.ReviewPlan)); err != nil {
		return err
	}
	if len(report.FirstReadQueue) > 0 {
		if _, err := fmt.Fprintln(w, "first read queue:"); err != nil {
			return err
		}
		for _, group := range report.FirstReadQueue {
			if _, err := fmt.Fprintf(w, "  - %s lane=%s files=%d\n", group.Group, group.Lane, len(group.Files)); err != nil {
				return err
			}
			for i, item := range group.ReadNext {
				if i >= 2 {
					break
				}
				if _, err := fmt.Fprintf(w, "      read %s:%d-%d %s\n", item.File, item.StartLine, item.EndLine, item.Reason); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func printAuditRisks(w io.Writer, report repomap.AuditRiskReport) error {
	if _, err := fmt.Fprintf(w, "audit risks: %s\n", report.Root); err != nil {
		return err
	}
	if len(report.Lanes) > 0 {
		if _, err := fmt.Fprintln(w, "lanes:"); err != nil {
			return err
		}
		for _, lane := range report.Lanes {
			if _, err := fmt.Fprintf(w, "  - %s: %s\n", lane.Name, lane.Reason); err != nil {
				return err
			}
		}
	}
	if len(report.Files) > 0 {
		if _, err := fmt.Fprintln(w, "files:"); err != nil {
			return err
		}
		for _, file := range report.Files {
			if _, err := fmt.Fprintf(w, "  - %s score=%d lanes=%v\n", file.Path, file.AuditScore, file.Lanes); err != nil {
				return err
			}
			for _, reason := range file.Reasons {
				if _, err := fmt.Fprintf(w, "      %s\n", reason); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func printAuditSurface(w io.Writer, report repomap.AuditSurfaceReport) error {
	if _, err := fmt.Fprintf(w, "audit surface: %s\n", report.Root); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  commands=%d flags=%d env=%d config=%d schema=%d routes=%d outputs=%d manifests=%d\n",
		len(report.Commands), len(report.Flags), len(report.EnvVars), len(report.ConfigKeys), len(report.SchemaFields), len(report.Routes), len(report.Outputs), len(report.DependencyManifests)); err != nil {
		return err
	}
	for _, file := range report.Files {
		if _, err := fmt.Fprintf(w, "  - %s score=%d kinds=%v\n", file.Path, file.Score, file.Kinds); err != nil {
			return err
		}
		for _, hit := range file.Hits {
			if _, err := fmt.Fprintf(w, "      %s %s line=%d lane=%s\n", hit.Kind, hit.Name, hit.Line, hit.Lane); err != nil {
				return err
			}
		}
	}
	return nil
}

func printAuditEffects(w io.Writer, report repomap.AuditEffectReport) error {
	if _, err := fmt.Fprintf(w, "audit effects: %s\n", report.Root); err != nil {
		return err
	}
	if len(report.Kinds) > 0 {
		if _, err := fmt.Fprintln(w, "kinds:"); err != nil {
			return err
		}
		for _, kind := range report.Kinds {
			if _, err := fmt.Fprintf(w, "  - %s lane=%s files=%d\n", kind.Name, kind.Lane, len(kind.Files)); err != nil {
				return err
			}
		}
	}
	if len(report.Files) > 0 {
		if _, err := fmt.Fprintln(w, "files:"); err != nil {
			return err
		}
		for _, file := range report.Files {
			if _, err := fmt.Fprintf(w, "  - %s score=%d lanes=%v\n", file.Path, file.Score, file.Lanes); err != nil {
				return err
			}
			for _, effect := range file.Effects {
				if _, err := fmt.Fprintf(w, "      %s %s line=%d lane=%s\n", effect.Kind, effect.Op, effect.Line, effect.Lane); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
