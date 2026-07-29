package repomap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const (
	defaultTaskTokens = 4096
	taskTargetLimit   = 6
)

type TaskOptions struct {
	MaxTokens     int
	ConsumedPaths []string
}

// TaskReport is schema version 1. The top-level JSON keys are a compatibility contract.
type TaskReport struct {
	SchemaVersion    int                 `json:"schema_version"`
	Root             string              `json:"root"`
	Goal             string              `json:"goal"`
	Budget           TaskBudget          `json:"budget"`
	Selection        TaskSelection       `json:"selection"`
	Rules            []TaskRule          `json:"rules"`
	RelatedChanges   []TaskRelatedChange `json:"related_changes"`
	Targets          []TaskTarget        `json:"targets"`
	ReadNext         []ReadNextItem      `json:"read_next"`
	VerifyCommands   []string            `json:"verify_commands"`
	FollowUpCommands []string            `json:"follow_up_commands"`
	Diagnostics      []string            `json:"diagnostics"`
	Truncations      []TaskTruncation    `json:"truncations"`
}
type TaskBudget struct {
	MaxTokens  int `json:"max_tokens"`
	UsedTokens int `json:"used_tokens"`
}
type TaskSelection struct {
	Strategy string `json:"strategy"`
	Limit    int    `json:"limit"`
	Selected int    `json:"selected"`
}
type TaskRule struct {
	Path string `json:"path"`
}
type TaskRelatedChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}
type TaskTruncation struct {
	Field  string `json:"field"`
	Shown  int    `json:"shown"`
	Total  int    `json:"total"`
	Reason string `json:"reason"`
}
type TaskEvidence struct {
	Field string `json:"field"`
	Value string `json:"value"`
}
type TaskRelationship struct {
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	Symbol     string `json:"symbol,omitempty"`
	Provenance string `json:"provenance"`
}
type TaskSource struct {
	Symbol string       `json:"symbol"`
	Lines  []SourceLine `json:"lines"`
}
type TaskEffect struct {
	Effect     AuditEffect `json:"effect"`
	Provenance string      `json:"provenance"`
}
type TaskTarget struct {
	Path             string             `json:"path"`
	Package          string             `json:"package"`
	AffectedPackages []string           `json:"affected_packages"`
	Confidence       string             `json:"confidence"`
	Symbols          []Symbol           `json:"symbols"`
	Evidence         []TaskEvidence     `json:"evidence"`
	Relationships    []TaskRelationship `json:"relationships"`
	Consumers        []string           `json:"consumers"`
	Callers          []Location         `json:"callers"`
	Tests            []string           `json:"tests"`
	Imports          []string           `json:"imports"`
	Effects          []TaskEffect       `json:"effects"`
	Boundaries       []string           `json:"boundaries"`
	Risk             string             `json:"risk"`
	Parse            string             `json:"parse"`
	Source           []TaskSource       `json:"source"`
	Consumed         bool               `json:"consumed"`
}

func (m *Map) Task(ctx context.Context, goal string, opts TaskOptions) (TaskReport, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return TaskReport{}, errors.New("task goal must not be blank")
	}
	if opts.MaxTokens < 0 {
		return TaskReport{}, errors.New("task max tokens must not be negative")
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = defaultTaskTokens
	}
	m.mu.RLock()
	root := m.root
	cfg := m.config
	m.mu.RUnlock()
	consumed, err := normalizeTaskPaths(root, opts.ConsumedPaths)
	if err != nil {
		return TaskReport{}, err
	}
	cfg.MaxTokens = opts.MaxTokens
	cfg.MaxTokensNoCtx = opts.MaxTokens
	cfg.Intent = goal
	cfg.ConsumedPaths = consumed
	cfg.SymbolRefs = false
	cfg.Explain = false
	cfg.IncludeTests = false
	cfg.GoAnalysis = true
	cfg.GoAnalysisCalls = true
	cfg.GoAnalysisTests = true
	build := New(root, cfg)
	if err := build.Build(ctx); err != nil {
		return TaskReport{}, fmt.Errorf("build task map: %w", err)
	}
	report := TaskReport{SchemaVersion: 1, Root: root, Goal: goal, Budget: TaskBudget{MaxTokens: opts.MaxTokens}, Selection: TaskSelection{Strategy: "positive task relevance, structural score, path", Limit: taskTargetLimit}, Rules: []TaskRule{}, RelatedChanges: []TaskRelatedChange{}, Targets: []TaskTarget{}, ReadNext: []ReadNextItem{}, VerifyCommands: []string{}, FollowUpCommands: []string{}, Diagnostics: []string{}, Truncations: []TaskTruncation{}}
	changes, gitDiagnostics := taskRelatedChanges(ctx, root)
	report.Diagnostics = append(report.Diagnostics, gitDiagnostics...)
	report.Diagnostics = append(report.Diagnostics, taskAnalysisDiagnostics(build)...)
	effects, effectErr := build.AuditEffects(ctx, 0)
	if effectErr != nil {
		report.Diagnostics = append(report.Diagnostics, "effects degraded: "+effectErr.Error())
	}
	effectByPath := taskEffectsByPath(effects)
	candidates := taskCandidates(build.Ranked(), goal)
	unreadSources := 0
	for _, candidate := range candidates {
		if len(report.Targets) == taskTargetLimit {
			report.addTruncation("targets", len(report.Targets), len(candidates), "target limit")
			break
		}
		target, truncations := buildTaskTarget(build, candidate, consumed, effectByPath)
		if target.Consumed || unreadSources >= 3 {
			if len(target.Source) > 0 {
				report.addTruncation("targets["+target.Path+"].source", 0, len(target.Source), "source target cap")
			}
			target.Source = nil
		} else {
			unreadSources++
		}
		packed := false
		for _, variant := range taskTargetPackingVariants(target, truncations) {
			trial := report
			trial.Targets = append(append([]TaskTarget(nil), report.Targets...), variant.target)
			trial.Truncations = append(append([]TaskTruncation(nil), report.Truncations...), variant.truncations...)
			if taskFits(trial) {
				report = trial
				packed = true
				break
			}
		}
		if !packed {
			report.addTruncation("targets", len(report.Targets), len(candidates), "token budget")
			break
		}
	}
	report.Selection.Selected = len(report.Targets)
	report.Rules = taskRules(root, report.Targets)
	report.RelatedChanges = filterTaskRelatedChanges(changes, report.Targets)
	for _, target := range report.Targets {
		for _, item := range taskReadNext(target) {
			if len(report.ReadNext) == 5 {
				report.addTruncation("read_next", len(report.ReadNext), len(report.ReadNext)+1, "read_next cap")
				break
			}
			report.ReadNext = append(report.ReadNext, item)
		}
	}
	report.VerifyCommands = taskVerifyCommands(report.Targets)
	report.FollowUpCommands = taskFollowUpCommands(report, consumed)
	if !taskFits(report) {
		report.addTruncation("related_changes", 0, len(report.RelatedChanges), "metadata packing")
		report.RelatedChanges = nil
	}
	if !taskFits(report) {
		report.addTruncation("read_next", 0, len(report.ReadNext), "metadata packing")
		report.ReadNext = nil
	}
	if len(report.Truncations) > 0 && len(report.FollowUpCommands) == 0 {
		report.FollowUpCommands = taskFollowUpCommands(report, consumed)
	}
	if !taskFits(report) {
		report.addTruncation("verify_commands", 0, len(report.VerifyCommands), "metadata packing")
		report.VerifyCommands = nil
	}
	if !taskFits(report) {
		report.addTruncation("follow_up_commands", 0, len(report.FollowUpCommands), "metadata packing")
		report.FollowUpCommands = nil
	}
	for !taskFits(report) && len(report.Targets) > 0 {
		report.Targets = report.Targets[:len(report.Targets)-1]
		report.Selection.Selected = len(report.Targets)
		report.addTruncation("targets", len(report.Targets), len(candidates), "final report packing")
		report.Rules = taskRules(root, report.Targets)
		report.RelatedChanges = filterTaskRelatedChanges(changes, report.Targets)
		report.ReadNext = []ReadNextItem{}
		report.VerifyCommands = []string{}
	}
	if !taskFits(report) {
		return TaskReport{}, fmt.Errorf("task token budget %d cannot encode report schema", opts.MaxTokens)
	}
	for range 4 {
		report.Budget.UsedTokens = taskOutputTokens(report)
	}
	if !taskFits(report) {
		return TaskReport{}, fmt.Errorf("task token budget %d cannot encode final report", opts.MaxTokens)
	}
	normalizeTaskReportSlices(&report)
	return report, nil
}

func (r *TaskReport) addTruncation(field string, shown, total int, reason string) {
	if total > shown {
		r.Truncations = append(r.Truncations, TaskTruncation{Field: field, Shown: shown, Total: total, Reason: reason})
	}
}

type taskTargetPackingVariant struct {
	target      TaskTarget
	truncations []TaskTruncation
}

func taskTargetPackingVariants(target TaskTarget, base []TaskTruncation) []taskTargetPackingVariant {
	variants := []taskTargetPackingVariant{{target: target, truncations: append([]TaskTruncation(nil), base...)}}
	primary := target
	primaryTruncations := append([]TaskTruncation(nil), base...)
	if len(primary.Source) > 1 {
		total := len(primary.Source)
		primary.Source = append([]TaskSource(nil), primary.Source[:1]...)
		primaryTruncations = append(primaryTruncations, TaskTruncation{
			Field: "targets[" + target.Path + "].source", Shown: 1, Total: total, Reason: "secondary source packing",
		})
		variants = append(variants, taskTargetPackingVariant{target: primary, truncations: primaryTruncations})
	}

	identity := primary
	identityTruncations := append([]TaskTruncation(nil), primaryTruncations...)
	taskOmitTargetSlice(&identityTruncations, "targets["+target.Path+"].relationships", len(identity.Relationships))
	taskOmitTargetSlice(&identityTruncations, "targets["+target.Path+"].consumers", len(identity.Consumers))
	taskOmitTargetSlice(&identityTruncations, "targets["+target.Path+"].callers", len(identity.Callers))
	taskOmitTargetSlice(&identityTruncations, "targets["+target.Path+"].tests", len(identity.Tests))
	taskOmitTargetSlice(&identityTruncations, "targets["+target.Path+"].imports", len(identity.Imports))
	taskOmitTargetSlice(&identityTruncations, "targets["+target.Path+"].effects", len(identity.Effects))
	identity.Relationships = nil
	identity.Consumers = nil
	identity.Callers = nil
	identity.Tests = nil
	identity.Imports = nil
	identity.Effects = nil
	variants = append(variants, taskTargetPackingVariant{target: identity, truncations: identityTruncations})

	if len(identity.Source) > 0 {
		minimum := identity
		minimum.Source = nil
		minimumTruncations := append([]TaskTruncation(nil), identityTruncations...)
		minimumTruncations = append(minimumTruncations, TaskTruncation{
			Field: "targets[" + target.Path + "].source", Shown: 0, Total: len(identity.Source), Reason: "source packing",
		})
		variants = append(variants, taskTargetPackingVariant{target: minimum, truncations: minimumTruncations})
	}
	return variants
}

func taskOmitTargetSlice(truncations *[]TaskTruncation, field string, total int) {
	if total > 0 {
		*truncations = append(*truncations, TaskTruncation{Field: field, Shown: 0, Total: total, Reason: "relationship packing"})
	}
}
func MarshalTaskJSON(report TaskReport) ([]byte, error) {
	normalizeTaskReportSlices(&report)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
func WriteTaskJSON(w io.Writer, report TaskReport) error {
	data, err := MarshalTaskJSON(report)
	if err != nil {
		return err
	}
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
func taskOutputTokens(report TaskReport) int {
	data, err := MarshalTaskJSON(report)
	if err != nil {
		return 0
	}
	n := len(data)
	if human := len(FormatTask(report)); human > n {
		n = human
	}
	return (n + 3) / 4
}
func taskFits(report TaskReport) bool { return taskOutputTokens(report) <= report.Budget.MaxTokens }

func normalizeTaskReportSlices(report *TaskReport) {
	if report.Rules == nil {
		report.Rules = []TaskRule{}
	}
	if report.RelatedChanges == nil {
		report.RelatedChanges = []TaskRelatedChange{}
	}
	if report.Targets == nil {
		report.Targets = []TaskTarget{}
	}
	if report.ReadNext == nil {
		report.ReadNext = []ReadNextItem{}
	}
	if report.VerifyCommands == nil {
		report.VerifyCommands = []string{}
	}
	if report.FollowUpCommands == nil {
		report.FollowUpCommands = []string{}
	}
	if report.Diagnostics == nil {
		report.Diagnostics = []string{}
	}
	if report.Truncations == nil {
		report.Truncations = []TaskTruncation{}
	}
	for i := range report.Targets {
		normalizeTaskTargetSlices(&report.Targets[i])
	}
}

func normalizeTaskTargetSlices(target *TaskTarget) {
	if target.AffectedPackages == nil {
		target.AffectedPackages = []string{}
	}
	if target.Symbols == nil {
		target.Symbols = []Symbol{}
	}
	if target.Evidence == nil {
		target.Evidence = []TaskEvidence{}
	}
	if target.Relationships == nil {
		target.Relationships = []TaskRelationship{}
	}
	if target.Consumers == nil {
		target.Consumers = []string{}
	}
	if target.Callers == nil {
		target.Callers = []Location{}
	}
	if target.Tests == nil {
		target.Tests = []string{}
	}
	if target.Imports == nil {
		target.Imports = []string{}
	}
	if target.Effects == nil {
		target.Effects = []TaskEffect{}
	}
	if target.Boundaries == nil {
		target.Boundaries = []string{}
	}
	if target.Source == nil {
		target.Source = []TaskSource{}
	}
	for i := range target.Source {
		if target.Source[i].Lines == nil {
			target.Source[i].Lines = []SourceLine{}
		}
	}
}

func normalizeTaskPaths(root string, paths []string) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve task root: %w", err)
	}
	seen := map[string]bool{}
	out := []string{}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			return nil, errors.New("consumed path must not be blank")
		}
		abs := path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
		rel, err := filepath.Rel(root, filepath.Clean(abs))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("consumed path %q is outside task root", path)
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	slices.Sort(out)
	return out, nil
}
func taskRules(root string, targets []TaskTarget) []TaskRule {
	root, _ = filepath.Abs(root)
	directories := []string{root}
	for _, target := range targets {
		dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(target.Path)))
		for {
			directories = append(directories, dir)
			if dir == root {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir || !taskPathWithin(root, dir) {
				break
			}
			dir = parent
		}
	}
	seen := map[string]bool{}
	out := []TaskRule{}
	foundAgents := false
	for _, start := range directories {
		for dir := start; ; dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, "AGENTS.md")
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				foundAgents = true
				path := taskDisplayPath(root, candidate)
				if !seen[path] {
					seen[path] = true
					out = append(out, TaskRule{Path: path})
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	optional := []string{".cursorrules", filepath.Join(".github", "copilot-instructions.md")}
	if !foundAgents {
		optional = append(optional, "CLAUDE.md")
	}
	for _, rel := range optional {
		candidate := filepath.Join(root, rel)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			path := filepath.ToSlash(rel)
			if !seen[path] {
				seen[path] = true
				out = append(out, TaskRule{Path: path})
			}
		}
	}
	slices.SortFunc(out, func(a, b TaskRule) int { return strings.Compare(a.Path, b.Path) })
	return out
}

func taskPathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func taskDisplayPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err == nil && taskPathWithin(root, path) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}
func filterTaskRelatedChanges(changes []TaskRelatedChange, targets []TaskTarget) []TaskRelatedChange {
	allowed := map[string]bool{}
	for _, t := range targets {
		allowed[t.Path] = true
		for _, p := range t.Consumers {
			allowed[p] = true
		}
		for _, p := range t.Tests {
			allowed[p] = true
		}
		for _, caller := range t.Callers {
			allowed[caller.File] = true
		}
		for _, relationship := range t.Relationships {
			allowed[relationship.Path] = true
		}
	}
	out := []TaskRelatedChange{}
	for _, c := range changes {
		if allowed[c.Path] {
			out = append(out, c)
		}
	}
	return out
}
func taskRelatedChanges(ctx context.Context, root string) ([]TaskRelatedChange, []string) {
	out, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain", "-z", "--untracked-files=all").Output()
	if err != nil {
		return nil, []string{"git status unavailable; related_changes omitted"}
	}
	changes := []TaskRelatedChange{}
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) >= 4 {
			changes = append(changes, TaskRelatedChange{Status: strings.TrimSpace(part[:2]), Path: filepath.ToSlash(part[3:])})
			if part[0] == 'R' || part[0] == 'C' {
				i++ // porcelain -z stores the source path in the next record
			}
		}
	}
	slices.SortFunc(changes, func(a, b TaskRelatedChange) int {
		return strings.Compare(a.Path+"\x00"+a.Status, b.Path+"\x00"+b.Status)
	})
	return changes, nil
}
