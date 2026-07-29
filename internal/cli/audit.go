package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/dotcommander/repomap"
)

type auditCommand struct {
	Hygiene auditHygieneCommand `cmd:"" help:"Report tracked, untracked, and ignored source-file hygiene"`
	Brief   auditBriefCommand   `cmd:"" help:"Report risks, surfaces, effects, and first-read queue in one map build"`
	Risks   auditRisksCommand   `cmd:"" help:"Report risk-ranked files and suggested deep-audit lanes"`
	Surface auditSurfaceCommand `cmd:"" help:"Report deterministic command, flag, config, schema, route, and output surfaces"`
	Effects auditEffectsCommand `cmd:"" help:"Report deterministic side-effect and trust-boundary packets"`
}

type auditHygieneCommand struct {
	Directory string `arg:"" optional:"" type:"path" default:"." help:"Directory to audit"`
	JSON      bool   `help:"Emit machine-readable audit hygiene JSON"`
}

func (c *auditHygieneCommand) Run(ctx context.Context, ioctx *commandIO) error {
	root, err := auditRoot(c.Directory)
	if err != nil {
		return err
	}
	report, err := repomap.AuditHygiene(ctx, root)
	if err != nil {
		return err
	}
	if c.JSON {
		return encodeAuditJSON(ioctx.stdout, report)
	}
	return printAuditHygiene(ioctx.stdout, report)
}

type auditMapOptions struct {
	Limit     int    `default:"20" help:"Maximum files to emit (0 = all)"`
	TopFiles  int    `name:"top-files" help:"Alias for --limit; maximum files to emit (0 = use --limit)"`
	Intent    string `short:"i" help:"Optional audit intent used to rerank files before packet generation"`
	JSON      bool   `help:"Emit machine-readable audit JSON"`
	Directory string `arg:"" optional:"" type:"path" default:"." help:"Directory to audit"`
}
type auditBriefCommand struct {
	auditMapOptions
}
type auditRisksCommand struct {
	auditMapOptions
}
type auditSurfaceCommand struct {
	auditMapOptions
}
type auditEffectsCommand struct {
	auditMapOptions
	Kind      string `help:"Filter effects by kind (for example database, subprocess, filesystem-write)"`
	PathsOnly bool   `name:"paths-only" help:"Emit only matching file paths"`
}

func (c *auditMapOptions) Validate() error {
	for _, value := range []struct {
		name  string
		value int
	}{
		{"limit", c.Limit},
		{"top-files", c.TopFiles},
	} {
		if value.value < 0 {
			return fmt.Errorf("--%s must be zero or greater", value.name)
		}
	}
	return nil
}

func (c *auditEffectsCommand) Validate() error {
	if err := c.auditMapOptions.Validate(); err != nil {
		return err
	}
	if err := validateAuditEffectKind(c.Kind); err != nil {
		return err
	}
	return nil
}

func (c *auditBriefCommand) Run(ctx context.Context, ioctx *commandIO) error {
	m, err := buildAuditMap(ctx, c.Directory, c.Intent)
	if err != nil {
		return err
	}
	report, err := m.AuditBrief(ctx, auditLimit(c.Limit, c.TopFiles))
	if err != nil {
		return err
	}
	if c.JSON {
		return encodeAuditJSON(ioctx.stdout, report)
	}
	return printAuditBrief(ioctx.stdout, report)
}
func (c *auditRisksCommand) Run(ctx context.Context, ioctx *commandIO) error {
	m, err := buildAuditMap(ctx, c.Directory, c.Intent)
	if err != nil {
		return err
	}
	report := m.AuditRisks(auditLimit(c.Limit, c.TopFiles))
	if c.JSON {
		return encodeAuditJSON(ioctx.stdout, report)
	}
	return printAuditRisks(ioctx.stdout, report)
}
func (c *auditSurfaceCommand) Run(ctx context.Context, ioctx *commandIO) error {
	m, err := buildAuditMap(ctx, c.Directory, c.Intent)
	if err != nil {
		return err
	}
	report, err := m.AuditSurface(ctx, auditLimit(c.Limit, c.TopFiles))
	if err != nil {
		return err
	}
	if c.JSON {
		return encodeAuditJSON(ioctx.stdout, report)
	}
	return printAuditSurface(ioctx.stdout, report)
}
func (c *auditEffectsCommand) Run(ctx context.Context, ioctx *commandIO) error {
	m, err := buildAuditMap(ctx, c.Directory, c.Intent)
	if err != nil {
		return err
	}
	report, err := m.AuditEffects(ctx, 0)
	if err != nil {
		return err
	}
	report = filterAuditEffects(report, c.Kind)
	report = limitAuditEffects(report, auditLimit(c.Limit, c.TopFiles))
	if c.PathsOnly {
		return printAuditEffectPaths(ioctx.stdout, report, c.JSON)
	}
	if c.JSON {
		return encodeAuditJSON(ioctx.stdout, report)
	}
	return printAuditEffects(ioctx.stdout, report)
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
	report.Truncations = slices.DeleteFunc(report.Truncations, func(truncation repomap.AuditTruncation) bool {
		return strings.HasSuffix(truncation.Field, "].effects")
	})
	for _, file := range report.Files {
		sourceEffects := file.AllEffects()
		effects := make([]repomap.AuditEffect, 0, len(sourceEffects))
		for _, effect := range sourceEffects {
			if effect.Kind != kind {
				continue
			}
			effects = append(effects, effect)
			kindFiles[effect.Kind] = append(kindFiles[effect.Kind], file.Path)
		}
		if len(effects) == 0 {
			continue
		}
		total := len(effects)
		file.Effects = effects
		file.OmittedReason = ""
		if total > 12 {
			file.Effects = file.Effects[:12]
			file.OmittedReason = fmt.Sprintf("showing 12 of %d effects; truncated by effects cap", total)
			report.Truncations = append(report.Truncations, repomap.AuditTruncation{
				Field: "files[" + file.Path + "].effects", Shown: 12, Total: total, Reason: "effects per-file cap",
			})
		}
		file.Lanes = auditEffectLanes(effects)
		files = append(files, file)
	}
	existingKinds := report.Kinds
	report.Files = files
	report.Kinds = nil
	report.Kinds = append(report.Kinds, buildFilteredEffectKinds(existingKinds, kindFiles)...)
	if len(report.Files) == 0 {
		report.FilesOmittedReason = "no side-effect data matched --kind"
	} else {
		report.FilesOmittedReason = ""
	}
	return report
}

func limitAuditEffects(report repomap.AuditEffectReport, limit int) repomap.AuditEffectReport {
	report.Truncations = slices.DeleteFunc(report.Truncations, func(truncation repomap.AuditTruncation) bool {
		return truncation.Field == "files" && truncation.Reason == "--limit"
	})
	total := len(report.Files)
	if limit > 0 && total > limit {
		report.Files = report.Files[:limit]
		report.FilesOmittedReason = fmt.Sprintf("showing %d of %d files; truncated by --limit", len(report.Files), total)
		report.Truncations = append(report.Truncations, repomap.AuditTruncation{
			Field: "files", Shown: len(report.Files), Total: total, Reason: "--limit",
		})
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

func validateAuditEffectKind(kind string) error {
	normalized := normalizeAuditEffectKind(kind)
	if normalized == "" {
		return nil
	}
	if _, ok := auditEffectKindNames[normalized]; ok {
		return nil
	}
	return fmt.Errorf("--kind must be one of all, database, filesystem-write, filesystem-read, subprocess, process-exit, http, serialization, secret, crypto, time, randomness, context-background, goroutine, or unbounded-read")
}

var auditEffectKindNames = map[string]struct{}{
	"database": {}, "filesystem-write": {}, "filesystem-read": {}, "subprocess": {}, "process-exit": {},
	"http": {}, "serialization": {}, "secret": {}, "crypto": {}, "time": {}, "randomness": {},
	"context-background": {}, "goroutine": {}, "unbounded-read": {},
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

func buildAuditMap(ctx context.Context, directory, intent string) (*repomap.Map, error) {
	root, err := auditRoot(directory)
	if err != nil {
		return nil, err
	}
	cfg := repomap.Config{
		MaxTokens:      0,
		MaxTokensNoCtx: 0,
		Intent:         intent,
	}
	m := repomap.New(root, cfg)
	if err := m.Build(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

func auditRoot(directory string) (string, error) {
	if directory == "" {
		directory = "."
	}
	root, err := filepath.Abs(directory)
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
